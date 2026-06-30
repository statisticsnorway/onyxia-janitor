package main

import (
	"context"
	"fmt"
	"log/slog"
	"onyxia-janitor/pkg"
	"onyxia-janitor/pkg/action/notify"
	"onyxia-janitor/pkg/action/setonyxiasecret"
	"onyxia-janitor/pkg/action/setvalues"
	"onyxia-janitor/pkg/action/uninstall"
	"onyxia-janitor/pkg/action/upgrade"
	"onyxia-janitor/pkg/daplaapi"
	"onyxia-janitor/pkg/onyxia"
	"onyxia-janitor/pkg/pipe"
	"os"
	"reflect"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/release"
	v1release "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"
	"github.com/expr-lang/expr"
)

type config struct {
	Action               string                         `env:"ACTION,required,notEmpty"`
	DryRun               bool                           `env:"DRY_RUN" envDefault:"false"`
	Catalogs             onyxia.Catalogs                `env:"ONYXIA_CATALOGS,required,notEmpty"`
	OnyxiaMetadataFilter pipe.Filter[onyxia.Service]    `env:"ONYXIA_METADATA_FILTER" envDefault:"true"`
	HelmReleaseFilter    pipe.Filter[v1release.Release] `env:"HELM_RELEASE_FILTER" envDefault:"true"`
	PushGatewayUrl       string                         `env:"PUSH_GATEWAY_URL"`
}

type notifyConfig struct {
	SubjectTemplate notify.EmailTemplate `env:"SUBJECT_TEMPLATE,required,notEmpty"`
	BodyTemplate    notify.EmailTemplate `env:"BODY_TEMPLATE,required,notEmpty"`
	DaplaApiUrl     string               `env:"DAPLA_API_URL,required,notEmpty"`
	DaplaApiSaToken string               `env:"DAPLA_API_SA_TOKEN,required,notEmpty"`
}

type setValuesConfig struct {
	ValuesMapper string `env:"VALUES_MAPPER,required,notEmpty"`
}

type upgradeConfig struct {
	Version string `env:"UPGRADE_VERSION,required,notEmpty"`
}

type setOnyxiaSecretConfig struct {
	SecretMapper string `env:"SECRET_MAPPER,required,notEmpty"`
}

func parseConfig[T any]() (T, error) {
	return env.ParseAsWithOptions[T](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: getLogLevelFromEnv()})))
	k8sClient, helmSettings := initializeKubernetesClient()
	ctx := context.Background()

	cfg := parseConfigOrExit()

	serviceAction, err := getServiceAction(cfg.Action, cfg.Catalogs, cfg.DryRun, k8sClient, helmSettings)
	if err != nil {
		slog.Error("unknown action", "action", cfg.Action)
		os.Exit(1)
	}
	slog.Info("determined action", "action", cfg.Action, "dry_run", cfg.DryRun)

	allNamespacesSecretLister := k8sClient.CoreV1().Secrets("")
	onyxiaClient := onyxia.New(allNamespacesSecretLister)

	outList := make(chan onyxia.Service, 10)
	go onyxiaClient.ListConcurrent(ctx, outList)
	slog.Info("started service lister")

	onyxiaMetaOut := make(chan onyxia.Service, 10)
	go cfg.OnyxiaMetadataFilter.Run(
		ctx,
		outList,
		onyxiaMetaOut,
		slog.With("filter", "onyxiaMetadata"),
	)
	slog.Info("started onyxia metadata filter")

	catalogOut := make(chan onyxia.Service, 10)
	catalogFilter := pipe.Filter[onyxia.Service](func(s onyxia.Service) bool {
		_, ok := cfg.Catalogs[s.Catalog]
		return ok
	})
	go catalogFilter.Run(
		ctx,
		onyxiaMetaOut,
		catalogOut,
		slog.With("filter", "catalog"),
	)
	slog.Info("started catalog filter")

	releaseMapper := onyxiaServiceToServiceWithRelease(helmSettings)
	releaseMapOut := make(chan onyxia.ServiceWithRelease, 10)
	go pipe.Map(ctx, releaseMapper, catalogOut, releaseMapOut)
	slog.Info("started release mapper")

	releaseFilter := pipe.Filter[onyxia.ServiceWithRelease](func(swr onyxia.ServiceWithRelease) bool {
		return serviceShouldBeIncluded(swr, helmSettings, cfg)
	})
	releaseFilterOut := make(chan onyxia.ServiceWithRelease, 10)
	go releaseFilter.Run(
		ctx,
		releaseMapOut,
		releaseFilterOut,
		slog.With("filter", "release"),
	)
	slog.Info("started release filter")

	for swr := range releaseFilterOut {
		log := swr.AddToLogger(slog.Default())
		if err := serviceAction.Process(ctx, swr); err != nil {
			log.Error("error processing service", "err", err)

		} else {
			log.Info("successfully processed service")
		}
	}

	result, err := serviceAction.Finish(ctx)
	if err != nil {
		slog.Error("failed to finish action", "err", err)
	}

	if cfg.PushGatewayUrl != "" {
		pushToPushgateway(cfg, result)
	}

	slog.Info("onyxia-janitor finished", "action", cfg.Action, "successful_count", result.SuccessfulCount, "failed_items_type", result.FailedItemsType, "failed_items", result.FailedItems)
}

func serviceShouldBeIncluded(swr onyxia.ServiceWithRelease, helmSettings *cli.EnvSettings, cfg config) bool {
	s := swr.Service
	actionConfig := &action.Configuration{}
	if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, ""); err != nil {
		slog.Error("init action config get metadata", "service", s)
		return false
	}
	client := action.NewGet(actionConfig)
	helmRelease, err := client.Run(s.Name)
	if err != nil {
		slog.Error("get helm release", "service", s)
		return false
	}
	v1HelmRelease, err := releaserToV1Release(helmRelease)
	if err != nil {
		slog.Error("cast helm release to manifest v1", "service", s)
		return false
	}

	if !cfg.Catalogs[s.Catalog].FilterRelease(*v1HelmRelease) {
		return false
	}
	return cfg.HelmReleaseFilter(*v1HelmRelease)
}

func onyxiaServiceToServiceWithRelease(helmSettings *cli.EnvSettings) func(s onyxia.Service) (*onyxia.ServiceWithRelease, error) {
	return func(s onyxia.Service) (*onyxia.ServiceWithRelease, error) {
		actionConfig := &action.Configuration{}
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, ""); err != nil {
			slog.Error("init action config get metadata", "service", s)
			return nil, err
		}
		client := action.NewGet(actionConfig)
		helmRelease, err := client.Run(s.Name)
		if err != nil {
			slog.Error("get helm release. this might be because the secret is present but the release is not installed", "service", s)
			return nil, err
		}
		v1HelmRelease, err := releaserToV1Release(helmRelease)
		if err != nil {
			slog.Error("cast helm release to manifest v1", "service", s)
			return nil, err
		}

		return &onyxia.ServiceWithRelease{
			Service: s,
			Release: *v1HelmRelease,
		}, nil
	}
}

func getServiceAction(action string, catalogs onyxia.Catalogs, dryRun bool, k8sClient *kubernetes.Clientset, helmSettings *cli.EnvSettings) (pkg.ServiceWithReleaseAction, error) {
	switch action {
	case "setvalues":
		setValuesConfig, err := parseConfig[setValuesConfig]()
		if err != nil {
			slog.Error("error reading setvalues config", "err", err)
			os.Exit(1)
		}
		prg, err := expr.Compile(setValuesConfig.ValuesMapper, expr.Env(onyxia.ServiceWithRelease{}), expr.AsKind(reflect.TypeOf(map[string]any{}).Kind()), expr.WarnOnAny())
		if err != nil {
			slog.Error("error parsing setvalues mapper", "err", err)
			os.Exit(1)
		}
		return setvalues.New(k8sClient, helmSettings, dryRun, catalogs, func(swr onyxia.ServiceWithRelease) (map[string]any, error) {
			res, err := expr.Run(prg, swr)
			if err != nil {
				return nil, err
			}
			return res.(map[string]any), nil
		}), nil
	case "setonyxiasecret":
		setOnyxiaSecretConfig, err := parseConfig[setOnyxiaSecretConfig]()
		if err != nil {
			slog.Error("error reading setonyxiasecret config", "err", err)
			os.Exit(1)
		}
		prg, err := expr.Compile(setOnyxiaSecretConfig.SecretMapper, expr.Env(onyxia.ServiceWithRelease{}), expr.AsKind(reflect.TypeOf(map[string]string{}).Kind()), expr.WarnOnAny())
		if err != nil {
			slog.Error("error parsing setonyxiasecret mapper", "err", err)
			os.Exit(1)
		}
		return setonyxiasecret.New(k8sClient, helmSettings, dryRun, catalogs, func(swr onyxia.ServiceWithRelease) (map[string]string, error) {
			res, err := expr.Run(prg, swr)
			if err != nil {
				return nil, err
			}
			return res.(map[string]string), nil
		}), nil
	case "upgrade":
		upgradeConfig, err := parseConfig[upgradeConfig]()
		if err != nil {
			slog.Error("error reading upgrade config", "err", err)
			os.Exit(1)
		}
		slog.Info("upgrade version " + upgradeConfig.Version)
		return upgrade.New(k8sClient, helmSettings, catalogs, upgradeConfig.Version, dryRun), nil
	case "uninstall":
		return uninstall.New(k8sClient, helmSettings, dryRun), nil
	case "notify":
		notifyCfg, err := parseConfig[notifyConfig]()
		if err != nil {
			slog.Error("error reading notify config", "err", err)
			os.Exit(1)
		}
		daplaApiClient := daplaapi.NewClient(
			notifyCfg.DaplaApiUrl,
			notifyCfg.DaplaApiSaToken,
		)
		return notify.New(daplaApiClient, daplaApiClient, notifyCfg.SubjectTemplate, notifyCfg.BodyTemplate, notify.WithDryRun(dryRun)), nil
	default:
		return nil, fmt.Errorf("unknown action %s", action)
	}
}

func pushToPushgateway(cfg config, result *pkg.ServiceWithReleaseActionResult) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.successItemsTotal.WithLabelValues(cfg.Action).Add(float64(result.SuccessfulCount))
	m.failedItemsCount.WithLabelValues(cfg.Action).Add(float64(len(result.FailedItems)))
	err := push.New(cfg.PushGatewayUrl, "onyxia-janitor").Gatherer(reg).Push()
	if err != nil {
		slog.Error("Failed to push to pushgateway", "err", err)
	}
}

func parseConfigOrExit() config {
	cfg, err := parseConfig[config]()
	if err != nil {
		slog.Error("parse config", "err", err.Error())
		os.Exit(1)
	}

	if err := cfg.Catalogs.Validate(); err != nil {
		slog.Error("catalog spec invalid", "err", err)
		os.Exit(1)
	}
	return cfg
}

// initializeKubernetesClient initializes the Kubernetes client and Helm settings
func initializeKubernetesClient() (*kubernetes.Clientset, *cli.EnvSettings) {
	var config *rest.Config
	var err error

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		slog.Info("Could not use local kubeconfig. Trying in-cluster configuration...", "error", err)
		config, err = rest.InClusterConfig()
		if err != nil {
			slog.Error("Could not use in-cluster configuration", "error", err)
			os.Exit(1)
		}
	}

	k8sClient := kubernetes.NewForConfigOrDie(config)
	settings := cli.New()
	settings.SetNamespace("")
	return k8sClient, settings
}

func getLogLevelFromEnv() slog.Level {
	levelStr := os.Getenv("GO_LOG")
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// https://github.com/helm/helm/blob/3120e88f9bab2aa0ce5d0dac1c467f4528054ed9/pkg/storage/driver/driver.go#L109
// releaserToV1Release is a helper function to convert a v1 release passed by interface
// into the type object.
func releaserToV1Release(rel release.Releaser) (*v1release.Release, error) {
	switch r := rel.(type) {
	case v1release.Release:
		return &r, nil
	case *v1release.Release:
		return r, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported release type: %T", rel)
	}
}

type Metrics struct {
	failedItemsCount  *prometheus.CounterVec
	successItemsTotal *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		failedItemsCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "onyxia_janitor_failed_items_total",
				Help: "Number of items that failed to be processed",
			},
			[]string{"action"},
		),
		successItemsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "onyxia_janitor_successful_count_total",
				Help: "Number of items that was processed successfully",
			},
			[]string{"action"},
		),
	}
	reg.MustRegister(m.failedItemsCount)
	reg.MustRegister(m.successItemsTotal)
	return m
}
