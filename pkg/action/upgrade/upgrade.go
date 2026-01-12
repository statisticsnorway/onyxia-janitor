package upgrade

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

type serviceUpgrader struct {
	catalogs     onyxia.Catalogs
	kubernetes   *kubernetes.Clientset
	helmSettings *cli.EnvSettings
	version      string
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, catalogs onyxia.Catalogs, version string) *serviceUpgrader {
	return &serviceUpgrader{
		kubernetes:   client,
		helmSettings: settings,
		catalogs:     catalogs,
		version:      version,
	}
}

func (s *serviceUpgrader) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())

	actionConfig := new(action.Configuration)
	s.helmSettings.SetNamespace(swr.Release.Namespace)
	if err := actionConfig.Init(s.helmSettings.RESTClientGetter(), swr.Release.Namespace, ""); err != nil {
		log.Error("error initializing config for release upgrade", "err", err)
		return err
	}
	log.Debug("attempting to upgrade release")

	values := map[string]any{}

	chartPathOptions := action.ChartPathOptions{
		// We here assume that catalog existence has been checked
		// in a previous filter stage.
		RepoURL: s.catalogs[swr.Service.Catalog].Url,
		Version: s.version,
	}
	chartPath, err := chartPathOptions.LocateChart(swr.Release.Chart.Metadata.Name, s.helmSettings)
	if err != nil {
		slog.Error("error locating chart for release", "err", err)
		return err
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		log.Error("error loading chart", "err", err)
		return err
	}

	upgradeAction := action.NewUpgrade(actionConfig)
	upgradeAction.Namespace = swr.Release.Namespace
	upgradeAction.Version = s.version
	upgradeAction.ReuseValues = true
	upgradeAction.ForceConflicts = true

	_, err = upgradeAction.Run(swr.Release.Name, chart, values)
	if err != nil {
		log.Error("error upgrading release", "err", err)
		return err
	}

	log.Info("successfully upgraded release")
	return nil
}

func (s *serviceUpgrader) Finish(ctx context.Context) error {
	return nil
}
