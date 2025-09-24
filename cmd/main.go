package main

import (
	"context"
	"fmt"
	"log/slog"
	"onyxia-janitor/pkg/action/notify"
	"onyxia-janitor/pkg/action/setvalues"
	"onyxia-janitor/pkg/action/uninstall"
	"onyxia-janitor/pkg/action/upgrade"
	"onyxia-janitor/pkg/onyxia"
	"onyxia-janitor/pkg/pipe"
	"onyxia-janitor/pkg/teamapi"
	"os"
	"reflect"
	"strings"

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
	Catalogs             onyxia.Catalogs                `env:"ONYXIA_CATALOGS,required,notEmpty"`
	OnyxiaMetadataFilter pipe.Filter[onyxia.Service]    `env:"ONYXIA_METADATA_FILTER" envDefault:"true"`
	HelmReleaseFilter    pipe.Filter[v1release.Release] `env:"HELM_RELEASE_FILTER" envDefault:"true"`
}

type notifyConfig struct {
	SubjectTemplate notify.EmailTemplate `env:"SUBJECT_TEMPLATE,required,notEmpty"`
	BodyTemplate    notify.EmailTemplate `env:"BODY_TEMPLATE,required,notEmpty"`
	ClientSecret    string               `env:"CLIENT_SECRET,required,notEmpty,unset"`
	ClientId        string               `env:"CLIENT_ID,required,notEmpty"`
	TokenUrl        string               `env:"TOKEN_URL,required,notEmpty"`
	TeamApiUrl      string               `env:"TEAM_API_URL,required,notEmpty"`
}

type setValuesConfig struct {
	ValuesMapper string `env:"VALUES_MAPPER,required,notEmpty"`
}

type upgradeConfig struct {
	Version string `env:"UPGRADE_VERSION,required,notEmpty"`
}

func parseConfig[T any]() (T, error) {
	return env.ParseAsWithOptions[T](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
}

type ServiceWithReleaseAction interface {
	Process(ctx context.Context, swr onyxia.ServiceWithRelease) error
	Finish(ctx context.Context) error
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: getLogLevelFromEnv()})))
	k8sClient, helmSettings := initializeKubernetesClient()
	ctx := context.Background()

	cfg := parseConfigOrExit()


	var serviceAction ServiceWithReleaseAction
	switch cfg.Action {
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
		serviceAction = setvalues.New(k8sClient, helmSettings, cfg.Catalogs, func(swr onyxia.ServiceWithRelease) (map[string]any, error) {
			res, err := expr.Run(prg, swr)
			if err != nil {
				return nil, err
			}
			return res.(map[string]any), nil
		})
	case "upgrade":
		upgradeConfig, err := parseConfig[upgradeConfig]()
		if err != nil {
			slog.Error("error reading upgrade config", "err", err)
			os.Exit(1)
		}
		slog.Info("upgrade version", upgradeConfig.Version)
		serviceAction = upgrade.New(k8sClient, helmSettings, cfg.Catalogs, upgradeConfig.Version)
	case "uninstall":
		serviceAction = uninstall.New(k8sClient, helmSettings)
	case "notify":
		notifyCfg, err := parseConfig[notifyConfig]()
		if err != nil {
			slog.Error("error reading notify config", "err", err)
			os.Exit(1)
		}
		teamapi := teamapi.NewClient(
			notifyCfg.TeamApiUrl,
			notifyCfg.TokenUrl,
			notifyCfg.ClientId,
			notifyCfg.ClientSecret,
		)
		serviceAction = notify.New(teamapi, teamapi, notifyCfg.SubjectTemplate, notifyCfg.BodyTemplate)
	default:
		slog.Error("unknown action", "action", cfg.Action)
		os.Exit(1)
	}
	slog.Info("determined action", "action", cfg.Action)

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

	releaseMapper := func(s onyxia.Service) (*onyxia.ServiceWithRelease, error) {
		actionConfig := &action.Configuration{}
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, ""); err != nil {
			slog.Error("init action config get metadata", "service", s)
			return nil, err
		}
		client := action.NewGet(actionConfig)
		helmRelease, err := client.Run(s.Name)
		if err != nil {
			slog.Error("get helm release", "service", s)
			return nil, err
		}
		release, err := releaserToV1Release(helmRelease)
		if err != nil {
			slog.Error("cast helm release to manifest v1", "service", s)
			return nil, err
		}

		return &onyxia.ServiceWithRelease{
			Service: s,
			Release: *release,
		}, nil
	}
	releaseMapOut := make(chan onyxia.ServiceWithRelease, 10)
	go pipe.Map(ctx, releaseMapper, catalogOut, releaseMapOut)
	slog.Info("started release mapper")

	releaseFilter := pipe.Filter[onyxia.ServiceWithRelease](func(swr onyxia.ServiceWithRelease) bool {
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
		release, err := releaserToV1Release(helmRelease)
		if err != nil {
			slog.Error("cast helm release to manifest v1", "service", s)
			return false
		}

		if !cfg.Catalogs[s.Catalog].FilterRelease(*release) {
			return false
		}
		return cfg.HelmReleaseFilter(*release)
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

	if err := serviceAction.Finish(ctx); err != nil {
		slog.Error("failed to finish action", "err", err)
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
		slog.Info("Could not use local kubeconfig: %v. Trying in-cluster configuration...", err)
		config, err = rest.InClusterConfig()
		if err != nil {
			slog.Error("Could not use in-cluster configuration: %v", err)
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
