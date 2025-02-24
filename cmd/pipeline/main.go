package main

import (
	"context"
	"log"
	"log/slog"
	"onyxia-janitor/pkg/filter"
	"onyxia-janitor/pkg/onyxia"
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
const helmReleaseFilter = `(not Config.global?.suspend ?? true) and (now().Sub(Info?.FirstDeployed.Time ?? now()) > duration(string(7 * 24) + "h"))`

type config struct {
	Action               string                         `env:"ACTION,required,notEmpty"`
	Catalogs             onyxia.Catalogs                `env:"ONYXIA_CATALOGS,required,notEmpty"`
	OnyxiaMetadataFilter filter.Filter[onyxia.Service]  `env:"ONYXIA_METADATA_FILTER" envDefault:"true"`
	HelmReleaseFilter    filter.Filter[release.Release] `env:"HELM_RELEASE_FILTER" envDefault:"true"`
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

	allNamespacesSecretLister := k8sClient.CoreV1().Secrets("")
	onyxiaClient := onyxia.New(allNamespacesSecretLister)

	services, err := onyxiaClient.List(ctx)
	if err != nil {
		panic(err)
	}
	slog.Info("before catalog filter", "serviceCount", len(services))
	services = filter.FilterSlice(nil, services, func(s onyxia.Service) bool {
		return cfg.Catalogs.Exists(s.Catalog)
	})

	slog.Info("before onyxiaMetaFilter", "serviceCount", len(services))
	services = cfg.OnyxiaMetadataFilter.FilterSlice(services)

	var releaseFilter filter.Filter[onyxia.Service] = func(s onyxia.Service) bool {
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
		} else if release == nil {
			slog.Error("helm release is nil", "service", s)
			return false
		}

		if !cfg.Catalogs[s.Catalog].FilterRelease(*release) {
			return false
		}

		return cfg.HelmReleaseFilter(*release)
	}
	slog.Info("before releaseFilter", "serviceCount", len(services))
	services = releaseFilter.FilterSlice(services)

	slog.Info("final", "serviceCount", len(services))
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
