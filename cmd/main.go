package main

import (
	"log"
	"os"

	"onyxia-janitor/pkg/action/notify"
	"onyxia-janitor/pkg/action/suspend"
	"onyxia-janitor/pkg/action/uninstall"
	"onyxia-janitor/pkg/teamapi"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caarlos0/env/v11"
)

type config struct {
	Action          string `env:"ACTION,required,notEmpty"`
	NamespacePrefix string `env:"NAMESPACE_PREFIX" envDefault:"user-ssb-"`
}

type suspendConfig struct {
	AllowedCharts []string `env:"ALLOWED_CHARTS,required,notEmpty"`
	HelmRepoUrl   string   `env:"HELM_REPO_URL,required,notEmpty"`
}

type uninstallConfig struct {
	AllowedCharts []string `env:"ALLOWED_CHARTS,required,notEmpty"`
}

type notifierConfig struct {
	TokenUrl     string `env:"TOKEN_URL,required,notEmpty"`
	ClientId     string `env:"CLIENT_ID,required,notEmpty"`
	ClientSecret string `env:"CLIENT_SECRET,required,notEmpty,unset"`
	TeamApiUrl   string `env:"TEAM_API_URL,required,notEmpty"`
}

func parseConfig[T any]() (T, error) {
	return env.ParseAsWithOptions[T](env.Options{
		Prefix: "ONYXIA_JANITOR_",
	})
}

func main() {
	cfg, err := parseConfig[config]()
	if err != nil {
		log.Printf("error parsing environment variables: %s", err)
		os.Exit(1)
	}

	log.Println("Starting Helm janitor process...")

	k8sClient, settings := initializeKubernetesClient()

	switch cfg.Action {
	case "uninstall":
		{
			log.Println("Running 'Uninstall failed releases' job...")
			uninstallCfg, err := parseConfig[uninstallConfig]()
			if err != nil {
				log.Printf("error parsing notifier environment variables: %s", err)
				os.Exit(1)
			}
			uninstaller := uninstall.New(k8sClient, settings)
			if err := uninstaller.UninstallFailedReleases(uninstallCfg.AllowedCharts, cfg.NamespacePrefix); err != nil {
				log.Printf("Error during 'Uninstall failed releases' job: %v", err)
			} else {
				log.Println("'Uninstall failed releases' job completed successfully")
			}
		}
	case "suspend":
		{
			log.Println("Running 'Suspend user services' job...")
			suspendCfg, err := parseConfig[suspendConfig]()
			if err != nil {
				log.Printf("error parsing notifier environment variables: %s", err)
				os.Exit(1)
			}
			suspender := suspend.New(k8sClient, settings, suspendCfg.HelmRepoUrl)
			if err := suspender.SuspendReleases(suspendCfg.AllowedCharts, cfg.NamespacePrefix); err != nil {
				log.Printf("Error during 'Suspend user services' job: %v", err)
			} else {
				log.Println("'Suspend user services' job completed successfully")
			}
		}
	case "notify":
		{
			log.Println("Running 'Notify users about old Helm releases' job...")
			notifyCfg, err := parseConfig[notifierConfig]()
			if err != nil {
				log.Printf("error parsing notifier environment variables: %s", err)
				os.Exit(1)
			}
			teamApiClient := teamapi.NewClient(notifyCfg.TeamApiUrl, notifyCfg.TokenUrl, notifyCfg.ClientId, notifyCfg.ClientSecret)
			notifier := notify.New(k8sClient, settings, teamApiClient)
			if err := notifier.NotifyUsersAboutOldServices(cfg.NamespacePrefix); err != nil {
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
