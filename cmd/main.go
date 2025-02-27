package main

import (
	"context"
	"log"
	"log/slog"
	"onyxia-janitor/pkg/action/notify"
	"onyxia-janitor/pkg/action/suspend"
	"onyxia-janitor/pkg/action/uninstall"
	"onyxia-janitor/pkg/onyxia"
	"onyxia-janitor/pkg/pipe"
	"onyxia-janitor/pkg/teamapi"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"
)

// const helmValuesFilter = ` global?.suspend ?? true`
const onyxiaMetadataFilter = `Namespace startsWith "user-ssb-"`

// const helmMetadataFilter = `true`
const helmReleaseFilter = `(now().Sub(Info?.FirstDeployed.Time ?? now()) > duration(string(7 * 24) + "h"))`

type config struct {
	Action               string                       `env:"ACTION,required,notEmpty"`
	Catalogs             onyxia.Catalogs              `env:"ONYXIA_CATALOGS,required,notEmpty"`
	OnyxiaMetadataFilter pipe.Filter[onyxia.Service]  `env:"ONYXIA_METADATA_FILTER" envDefault:"true"`
	HelmReleaseFilter    pipe.Filter[release.Release] `env:"HELM_RELEASE_FILTER" envDefault:"true"`
}

type notifyConfig struct {
	SubjectTemplate notify.EmailTemplate `env:"SUBJECT_TEMPLATE,required,notEmpty"`
	BodyTemplate    notify.EmailTemplate `env:"BODY_TEMPLATE,required,notEmpty"`
	ClientSecret    string               `env:"CLIENT_SECRET,required,notEmpty,unset"`
	ClientId        string               `env:"CLIENT_ID,required,notEmpty"`
	TokenUrl        string               `env:"TOKEN_URL,required,notEmpty"`
	TeamApiUrl      string               `env:"TEAM_API_URL,required,notEmpty"`
}

func parseConfig[T any]() (T, error) {
	return env.ParseAsWithOptions[T](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
}

func main() {
	k8sClient, helmSettings := initializeKubernetesClient()
	ctx := context.Background()

	cfg, err := parseConfig[config]()
	if err != nil {
		slog.Error("parse config", "err", err.Error())
		os.Exit(1)
	}

	if err := cfg.Catalogs.Validate(); err != nil {
		slog.Error("catalog spec invalid", "err", err)
		os.Exit(1)
	}

	var serviceAction ServiceWithReleaseAction
	switch cfg.Action {
	case "suspend":
		serviceAction = suspend.New(k8sClient, helmSettings, cfg.Catalogs)
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
	}

	allNamespacesSecretLister := k8sClient.CoreV1().Secrets("")
	onyxiaClient := onyxia.New(allNamespacesSecretLister)

	outList := make(chan onyxia.Service, 10)
	go onyxiaClient.ListConcurrent(ctx, outList)

	onyxiaMetaOut := make(chan onyxia.Service, 10)
	go cfg.OnyxiaMetadataFilter.Run(ctx, outList, onyxiaMetaOut)

	catalogOut := make(chan onyxia.Service, 10)
	catalogFilter := pipe.Filter[onyxia.Service](func(s onyxia.Service) bool {
		_, ok := cfg.Catalogs[s.Catalog]
		return ok
	})
	go catalogFilter.Run(ctx, onyxiaMetaOut, catalogOut)

	releaseMapper := func(s onyxia.Service) (*onyxia.ServiceWithRelease, error) {
		actionConfig := &action.Configuration{}
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, "", slog.Debug); err != nil {
			slog.Error("init action config get metadata", "service", s)
			return nil, err
		}
		client := action.NewGet(actionConfig)
		release, err := client.Run(s.Name)
		if err != nil {
			slog.Error("get helm release", "service", s)
			return nil, err
		}
		return &onyxia.ServiceWithRelease{
			Service: s,
			Release: *release,
		}, nil
	}
	releaseMapOut := make(chan onyxia.ServiceWithRelease, 10)
	go pipe.Map(ctx, releaseMapper, catalogOut, releaseMapOut)

	releaseFilter := pipe.Filter[onyxia.ServiceWithRelease](func(swr onyxia.ServiceWithRelease) bool {
		s := swr.Service
		actionConfig := &action.Configuration{}
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, "", slog.Debug); err != nil {
			slog.Error("init action config get metadata", "service", s)
			return false
		}
		client := action.NewGet(actionConfig)
		release, err := client.Run(s.Name)
		if err != nil {
			slog.Error("get helm release", "service", s)
			return false
		}
		if !cfg.Catalogs[s.Catalog].FilterRelease(*release) {
			return false
		}
		return cfg.HelmReleaseFilter(*release)
	})
	releaseFilterOut := make(chan onyxia.ServiceWithRelease, 10)
	go releaseFilter.Run(ctx, releaseMapOut, releaseFilterOut)

	for swr := range releaseFilterOut {
		log := swr.AddToLogger(slog.Default())
		if err := serviceAction.Process(ctx, swr); err != nil {
			log.Error("error processing service", "err", err)
		}
		log.Info("successfully processed service")
	}

	if err := serviceAction.Finish(ctx); err != nil {
		slog.Error("failed to finish action", "err", err)
	}
}

type ServiceWithReleaseAction interface {
	Process(ctx context.Context, swr onyxia.ServiceWithRelease) error
	Finish(ctx context.Context) error
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
		log.Printf("Could not use local kubeconfig: %v. Trying in-cluster configuration...", err)
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Could not use in-cluster configuration: %v", err)
		}
	}

	k8sClient := kubernetes.NewForConfigOrDie(config)
	settings := cli.New()
	settings.SetNamespace("")
	return k8sClient, settings
}
