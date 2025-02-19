package main

import (
	"context"
	"log"
	nsgetter "onyxia-janitor/pkg/namespaces"
	"onyxia-janitor/pkg/onyxia"
	"os"

	"helm.sh/helm/v3/pkg/cli"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1typed "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"

	corev1 "k8s.io/api/core/v1"
)

const namespaceFilter = `Name startsWith "user-ssb-tni"`

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

func main() {
	k8sClient, _ := initializeKubernetesClient()
	ctx := context.Background()

	filteredNamespaceLister, err := nsgetter.New(
		k8sClient.CoreV1().Namespaces(),
		nsgetter.WithExprFilter(namespaceFilter),
	)
	if err != nil {
		panic(err)
	}

	namespaces, err := filteredNamespaceLister.List(ctx)
	if err != nil {
		panic(err)
	}

	onyxiaClient := onyxia.New(k8sClient.CoreV1())
	catalogServiceCount := map[string]int{}
	for catalog := range catalogs {
		catalogServiceCount[catalog] = 0
	}

	for _, namespace := range namespaces {
		log.Printf("processing namespace %q", namespace.Name)
		services, err := onyxiaClient.List(ctx, namespace.Name)
		if err != nil {
			log.Printf("list services in namespace %q: %v", namespace.Name, err)
			continue
		}
		log.Printf("namespace %q has %d onyxia services", namespace.Name, len(services))
		for _, service := range services {
			if _, ok := catalogs[service.Catalog]; !ok {
				log.Printf("service %q has illegal catalog %q", service.Name, service.Catalog)
				continue
			}
			catalogServiceCount[service.Catalog] += 1
		}
	}

	for catalog, count := range catalogServiceCount {
		log.Printf("%s: %d", catalog, count)
	}
}

func Namespaces(ctx context.Context, namespaceClient corev1typed.NamespaceInterface) ([]corev1.Namespace, error) {
	var namespaces []corev1.Namespace
	cont := ""
	for {
		namespaceList, err := namespaceClient.List(context.Background(), v1.ListOptions{Continue: cont})
		if err != nil {
			return nil, err
		}

		namespaces = append(namespaces, namespaceList.Items...)
		cont = namespaceList.Continue
		if cont == "" {
			break
		}
	}
	return namespaces, nil
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
