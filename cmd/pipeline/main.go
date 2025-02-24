package main

import (
	"context"
	"log"
	"log/slog"
	"onyxia-janitor/pkg/onyxia"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"
	"github.com/expr-lang/expr"
)

// const helmValuesFilter = ` global?.suspend ?? true`
const onyxiaMetadataFilter = `Namespace startsWith "user-ssb-"`

// const helmMetadataFilter = `true`
const helmReleaseFilter = `(not Config.global?.suspend ?? true) and (now().Sub(Info?.FirstDeployed.Time ?? now()) > duration(string(7 * 24) + "h"))`

var catalogs map[string]string = map[string]string{
	"dapla-lab-standard":          "https://statisticsnorway.github.io/dapla-lab-helm-charts-standard",
	"dapla-lab-standard-test":     "https://statisticsnorway.github.io/dapla-lab-helm-charts-standard-test",
	"dapla-lab-services":          "https://statisticsnorway.github.io/dapla-lab-helm-charts-services",
	"dapla-lab-experimental":      "https://statisticsnorway.github.io/dapla-lab-helm-charts-experimental",
	"workshop-dapla-lab-services": "https://statisticsnorway.github.io/workshop-dapla-lab-helm-charts",
}

func parseConfig[T any]() (T, error) {
	return env.ParseAsWithOptions[T](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
}

func ParseFilter[T any](filter string) (func(T) bool, error) {
	prg, err := expr.Compile(filter, expr.Env(new(T)), expr.AsBool(), expr.WarnOnAny())
	if err != nil {
		return nil, err
	}

	return func(t T) bool {
		res, _ := expr.Run(prg, t)
		return res.(bool)
	}, nil
}

func Filter[T any](d []T, s []T, keep func(T) bool) []T {
	for _, t := range s {
		if keep(t) {
			d = append(d, t)
		}
	}
	return d
}

func main() {
	k8sClient, helmSettings := initializeKubernetesClient()
	ctx := context.Background()

	onyxiaMetaFilter, err := ParseFilter[onyxia.Service](onyxiaMetadataFilter)
	if err != nil {
		panic(err)
	}

	// helmMetaFilter, err := ParseFilter[action.Metadata](helmMetadataFilter)
	// if err != nil {
	// 	panic(err)
	// }

	helmReleaseFilter, err := ParseFilter[release.Release](helmReleaseFilter)
	if err != nil {
		panic(err)
	}

	allNamespacesSecretLister := k8sClient.CoreV1().Secrets("")
	onyxiaClient := onyxia.New(allNamespacesSecretLister)

	services, err := onyxiaClient.List(ctx)
	if err != nil {
		panic(err)
	}
	slog.Info("before onyxiaMetaFilter", "serviceCount", len(services))
	services = Filter(nil, services, onyxiaMetaFilter)

	slog.Info("before catalog filter", "serviceCount", len(services))
	services = Filter(nil, services, func(s onyxia.Service) bool {
		_, ok := catalogs[s.Catalog]
		return ok
	})

	// metadataFilter := func(s onyxia.Service) bool {
	// 	actionConfig := &action.Configuration{}
	// 	if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, "", slog.Debug); err != nil {
	// 		slog.Error("init action config get metadata", "service", s)
	// 		return false
	// 	}
	// 	client := action.NewGetMetadata(actionConfig)
	// 	metadata, err := client.Run(s.Name)
	// 	if err != nil {
	// 		slog.Error("get metadata", "service", s)
	// 		return false
	// 	}

	// 	return helmMetaFilter(*metadata)
	// }

	// slog.Info("before metadataFilter", "serviceCount", len(services))
	// services = Filter(nil, services, metadataFilter)

	// valuesFilter := func(s onyxia.Service) bool {
	// 	valuesLog := slog.With("service", s)
	// 	actionConfig := &action.Configuration{}
	// 	if err := actionConfig.Init(helmSettings.RESTClientGetter(), s.Namespace, "", slog.Debug); err != nil {
	// 		valuesLog.Error("init action config get metadata", "service", s)
	// 		return false
	// 	}
	// 	client := action.NewGetValues(actionConfig)
	// 	values, err := client.Run(s.Name)
	// 	if err != nil {
	// 		valuesLog.Error("get values", "err", err)
	// 		return false
	// 	}

	// 	res, err := expr.Eval(helmValuesFilter, values)
	// 	if err != nil {
	// 		valuesLog.Error("error in helm values filter", "filter", helmValuesFilter, "err", err)
	// 		return false
	// 	}

	// 	if b, ok := res.(bool); ok {
	// 		return b
	// 	}

	// 	valuesLog.Error("helm values filter does not evaluate to bool", "filter", helmValuesFilter, "type", reflect.TypeOf(res).String())
	// 	return false
	// }

	// slog.Info("before valuesFilter", "serviceCount", len(services))
	// services = Filter(nil, services, valuesFilter)

	releaseFilter := func(s onyxia.Service) bool {
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

		return helmReleaseFilter(*release)
	}
	slog.Info("before releaseFilter", "serviceCount", len(services))
	services = Filter(nil, services, releaseFilter)

	slog.Info("final", "serviceCount", len(services))
	catalogServiceCount := map[string]int{}
	for catalog := range catalogs {
		catalogServiceCount[catalog] = 0
	}
	for _, service := range services {
		slog.Info("look", "service", service)
		if _, ok := catalogs[service.Catalog]; !ok {
			log.Printf("service %q has illegal catalog %q", service.Name, service.Catalog)
			continue
		}
		catalogServiceCount[service.Catalog] += 1
	}

	for catalog, count := range catalogServiceCount {
		log.Printf("%s: %d", catalog, count)
	}
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
