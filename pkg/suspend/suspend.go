package suspend

import (
	"log"
	"onyxia-janitor/common"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
)

const helmRepoURL = "https://statisticsnorway.github.io/dapla-lab-helm-charts-standard"

func SuspendReleases(clientset *kubernetes.Clientset, helmSettings *cli.EnvSettings, allowedCharts []string, prefix string) error {
	namespaces, err := common.GetNamespacesWithPrefix(clientset, prefix)
	if err != nil {
		return err
	}

	for _, namespace := range namespaces {
		log.Printf("Processing namespace: %s", namespace)

		actionConfig := new(action.Configuration)
		helmSettings.SetNamespace(namespace)
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), namespace, "", log.Printf); err != nil {
			log.Printf("Error initializing Helm action configuration for namespace %s: %v", namespace, err)
			continue
		}

		if err := processReleases(actionConfig, namespace, allowedCharts); err != nil {
			log.Printf("Error suspending releases in namespace %s: %v", namespace, err)
		}
	}

	return nil
}

func processReleases(actionConfig *action.Configuration, namespace string, allowedCharts []string) error {
	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = false

	releases, err := listAction.Run()
	if err != nil {
		return err
	}

	log.Printf("Found %d releases in namespace %s", len(releases), namespace)

	for _, rel := range releases {
		if !common.IsAllowedChart(rel.Chart.Metadata.Name, allowedCharts) {
			log.Printf("Skipping release %s (chart: %s not in allowed list)", rel.Name, rel.Chart.Metadata.Name)
			continue
		}

		if err := suspendRelease(actionConfig, rel); err != nil {
			log.Printf("Error suspending release %s in namespace %s: %v", rel.Name, namespace, err)
		}
	}
	return nil
}

func suspendRelease(actionConfig *action.Configuration, rel *release.Release) error {
	log.Printf("Attempting to suspend release %s (chart: %s) in namespace %s", rel.Name, rel.Chart.Metadata.Name, rel.Namespace)

	os.Setenv("KUBERNETES_NAMESPACE", rel.Namespace)

	suspendValues := map[string]any{
		"global": map[string]any{
			"suspend": true,
		},
	}

	chartPathOptions := action.ChartPathOptions{
		RepoURL: helmRepoURL,
		Version: rel.Chart.Metadata.Version,
	}
	chartPath, err := chartPathOptions.LocateChart(rel.Chart.Metadata.Name, cli.New())
	if err != nil {
		log.Printf("Error locating chart for release %s: %v", rel.Name, err)
		return err
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		log.Printf("Error loading chart: %v", err)
		return err
	}

	upgradeAction := action.NewUpgrade(actionConfig)
	upgradeAction.Namespace = rel.Namespace
	upgradeAction.Version = rel.Chart.Metadata.Version
	upgradeAction.ReuseValues = true

	_, err = upgradeAction.Run(rel.Name, chart, suspendValues)
	if err != nil {
		log.Printf("Error upgrading release %s: %v", rel.Name, err)
		return err
	}

	log.Printf("Successfully suspended release: %s in namespace %s", rel.Name, rel.Namespace)
	return nil
}
