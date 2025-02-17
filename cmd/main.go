package main

import (
	"log"
	"os"
	"time"

	"onyxia-janitor/pkg/notify"
	"onyxia-janitor/pkg/suspend"
	"onyxia-janitor/pkg/teamapi"
	"onyxia-janitor/pkg/uninstall"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"
)

type config struct {
	HelmRepoUrl string `env:"HELM_REPO_URL,required,notEmpty"`
	Action      string `env:"ACTION,required,notEmpty"`
}

type notifierConfig struct {
	TokenUrl     string `env:"TOKEN_URL,required,notEmpty"`
	ClientId     string `env:"CLIENT_ID,required,notEmpty"`
	ClientSecret string `env:"CLIENT_SECRET,required,notEmpty,unset"`
	TeamApiUrl   string `env:"TEAM_API_URL,required,notEmpty"`
}

func main() {
	cfg, err := env.ParseAsWithOptions[config](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
	if err != nil {
		log.Printf("error parsing environment variables: %s", err)
		os.Exit(1)
	}

	notifyCfg, err := env.ParseAsWithOptions[notifierConfig](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
	if err != nil {
		log.Printf("error parsing notifier environment variables: %s", err)
		os.Exit(1)
	}

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

	switch cfg.Action {
	case "uninstall":
		{
			log.Println("Running 'Uninstall failed releases' job...")
			uninstaller := uninstall.New(k8sClient, settings)
			if err := uninstaller.UninstallFailedReleases(allowedCharts, "user-ssb-"); err != nil {
				log.Printf("Error during 'Uninstall failed releases' job: %v", err)
			} else {
				log.Println("'Uninstall failed releases' job completed successfully")
			}
		}
	case "suspend":
		{
			log.Println("Running 'Suspend user services' job...")
			suspender := suspend.New(k8sClient, settings, cfg.HelmRepoUrl)
			if err := suspender.SuspendReleases(allowedCharts, "user-ssb-"); err != nil {
				log.Printf("Error during 'Suspend user services' job: %v", err)
			} else {
				log.Println("'Suspend user services' job completed successfully")
			}
		}
	case "notify":
		{
			log.Println("Running 'Notify users about old Helm releases' job...")
			teamApiClient := teamapi.NewClient(notifyCfg.TeamApiUrl, notifyCfg.TokenUrl, notifyCfg.ClientId, notifyCfg.ClientSecret)
			notifier := notify.New(k8sClient, settings, teamApiClient)
			if err := notifier.NotifyUsersAboutOldServices("user-ssb-"); err != nil {
				log.Printf("Error during 'Notify users about old Helm releases' job: %v", err)
			} else {
				log.Println("'Notify users about old Helm releases' job completed successfully")
			}
		}
	default:
		log.Printf("unknown action %q, must be one of: [uninstall, suspend, notify]", cfg.Action)
		os.Exit(1)
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
