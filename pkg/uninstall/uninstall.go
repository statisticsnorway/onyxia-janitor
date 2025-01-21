package uninstall

import (
	"log"
	"onyxia-janitor/common"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
)

func UninstallFailedReleases(clientset *kubernetes.Clientset, helmSettings *cli.EnvSettings, allowedCharts []string, prefix string) error {
	namespaces, err := common.GetNamespacesWithPrefix(clientset, prefix)
	if err != nil {
		return err
	}

	for _, namespace := range namespaces {
		log.Printf("Processing namespace: %s", namespace)

		os.Setenv("KUBERNETES_NAMESPACE", namespace) // Ensure namespace context is set
		actionConfig := new(action.Configuration)
		if err := actionConfig.Init(helmSettings.RESTClientGetter(), namespace, "", log.Printf); err != nil {
			log.Printf("Error initializing Helm action configuration for namespace %s: %v", namespace, err)
			continue
		}

		if err := uninstallFailedReleasesInNamespace(actionConfig, namespace, allowedCharts); err != nil {
			log.Printf("Error uninstalling failed releases in namespace %s: %v", namespace, err)
		}
	}

	return nil
}

func uninstallFailedReleasesInNamespace(actionConfig *action.Configuration, namespace string, allowedCharts []string) error {
	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = false
	listAction.Filter = "^.*$"

	releases, err := listAction.Run()
	if err != nil {
		return err
	}

	for _, rel := range releases {
		if rel.Info.Status == release.StatusFailed && common.IsAllowedChart(rel.Chart.Metadata.Name, allowedCharts) {
			log.Printf("Uninstalling failed release %s (chart: %s) in namespace %s", rel.Name, rel.Chart.Metadata.Name, namespace)

			uninstallAction := action.NewUninstall(actionConfig)
			_, err := uninstallAction.Run(rel.Name)
			if err != nil {
				log.Printf("Error uninstalling release %s: %v", rel.Name, err)
			} else {
				log.Printf("Successfully uninstalled release %s in namespace %s", rel.Name, namespace)
			}
		}
	}
	return nil
}
