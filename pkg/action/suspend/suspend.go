package suspend

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

type serviceSuspender struct {
	catalogs     onyxia.Catalogs
	kubernetes   *kubernetes.Clientset
	helmSettings *cli.EnvSettings
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, catalogs onyxia.Catalogs) *serviceSuspender {
	return &serviceSuspender{
		kubernetes:   client,
		helmSettings: settings,
		catalogs:     catalogs,
	}
}

// Process sets .global.suspend=true for the given service.
func (s *serviceSuspender) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())

	actionConfig := new(action.Configuration)
	s.helmSettings.SetNamespace(swr.Release.Namespace)
	if err := actionConfig.Init(s.helmSettings.RESTClientGetter(), swr.Release.Namespace, ""); err != nil {
		log.Error("error initializing config for release suspension", "err", err)
		return err
	}
	log.Debug("attempting to suspend release")

	suspendValues := map[string]any{
		"global": map[string]any{
			"suspend": true,
		},
	}

	chartPathOptions := action.ChartPathOptions{
		// We here assume that catalog existence has been checked
		// in a previous filter stage.
		RepoURL: s.catalogs[swr.Service.Catalog].Url,
		Version: swr.Release.Chart.Metadata.Version,
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
	upgradeAction.Version = swr.Release.Chart.Metadata.Version
	upgradeAction.ReuseValues = true

	_, err = upgradeAction.Run(swr.Release.Name, chart, suspendValues)
	if err != nil {
		log.Error("error upgrading release", "err", err)
		return err
	}

	log.Info("successfully suspended release")
	return nil
}

func (s *serviceSuspender) Finish(ctx context.Context) error {
	return nil
}
