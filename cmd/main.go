package main

import (
	"log"
	"os"

	"onyxia-janitor/pkg/suspend"
	"onyxia-janitor/pkg/uninstall"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/jasonlvhit/gocron"
)

func main() {
	log.Println("Starting Helm janitor process...")

	// Initialize Kubernetes client and Helm settings
	k8sClient, settings := initializeKubernetesClient()

	// Allowed Helm charts
	allowedCharts := []string{
		"vscode-python",
		"rstudio",
		"jupyter",
		"jupyter-playground",
		"jdemetra",
		"jupyter-pyspark",
		"datadoc",
	}

	gocron.Every(1).Day().At("20:00").Do(func() {
		log.Println("Running 'Uninstall failed releases' job...")
		if err := uninstall.UninstallFailedReleases(k8sClient, settings, allowedCharts, "user-ssb-"); err != nil {
			log.Printf("Error during 'Uninstall failed releases' job: %v", err)
		} else {
			log.Println("'Uninstall failed releases' job completed successfully")
		}
	})

	gocron.Every(1).Day().At("22:00").Do(func() {
		log.Println("Running 'Suspend user services' job...")
		if err := suspend.SuspendReleases(k8sClient, settings, allowedCharts, "user-ssb-"); err != nil {
			log.Printf("Error during 'Suspend user services' job: %v", err)
		} else {
			log.Println("'Suspend user services' job completed successfully")
		}
	})

	gocron.Start()
	select {}
}

func initializeKubernetesClient() (*kubernetes.Clientset, *cli.EnvSettings) {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("Error building Kubernetes config: %v", err)
	}
	k8sClient := kubernetes.NewForConfigOrDie(config)
	settings := cli.New()
	return k8sClient, settings
}
