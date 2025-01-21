package main

import (
	"log"
	"os"
	"time"

	"onyxia-janitor/pkg/suspend"
	"onyxia-janitor/pkg/uninstall"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/jasonlvhit/gocron"
)

func main() {
	log.Println("Starting Helm janitor process...")

	log.Println("Current time:", time.Now())

	k8sClient, settings := initializeKubernetesClient()

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

	log.Println("All jobs scheduled. Waiting for the scheduled times to run...")
	gocron.Start()
	select {}
}

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
