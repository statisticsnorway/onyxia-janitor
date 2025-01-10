package main

import (
	"log"
	"os"
	"time"

	"onyxia-janitor/pkg/suspend"
	"onyxia-janitor/pkg/uninstall"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	log.Println("Starting Helm janitor process...")

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

	jobs := []struct {
		name     string
		schedule string
		jobFunc  func(*kubernetes.Clientset, *cli.EnvSettings, []string, string) error
	}{
		{"Uninstall failed releases", "20:00", uninstall.UninstallFailedReleases},
		{"Suspend user services", "22:00", suspend.SuspendReleases},
	}

	for _, job := range jobs {
		go scheduleJob(k8sClient, settings, allowedCharts, "user-ssb-", job.schedule, job.jobFunc, job.name)
	}

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

func scheduleJob(
	clientset *kubernetes.Clientset,
	settings *cli.EnvSettings,
	allowedCharts []string,
	prefix, schedule string,
	jobFunc func(*kubernetes.Clientset, *cli.EnvSettings, []string, string) error,
	jobName string,
) {
	for {
		nextRun := calculateNextRun(schedule)
		sleepDuration := time.Until(nextRun)

		log.Printf("%s job will run at %s (in %v)", jobName, nextRun, sleepDuration)
		time.Sleep(sleepDuration)

		log.Printf("Running %s job...", jobName)
		if err := jobFunc(clientset, settings, allowedCharts, prefix); err != nil {
			log.Printf("Error during %s job: %v", jobName, err)
		} else {
			log.Printf("%s job completed successfully", jobName)
		}
	}
}

func calculateNextRun(schedule string) time.Time {
	now := time.Now()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		log.Fatalf("Failed to load timezone Europe/Oslo: %v", err)
	}

	scheduleTime, err := time.ParseInLocation("15:04", schedule, loc)
	if err != nil {
		log.Fatalf("Failed to parse schedule time: %v", err)
	}

	nextRun := time.Date(now.Year(), now.Month(), now.Day(), scheduleTime.Hour(), scheduleTime.Minute(), 0, 0, loc)
	if now.After(nextRun) {
		nextRun = nextRun.Add(24 * time.Hour)
	}
	return nextRun
}
