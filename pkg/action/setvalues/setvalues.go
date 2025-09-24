package setvalues

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

type valueSetter struct {
	catalogs     onyxia.Catalogs
	kubernetes   *kubernetes.Clientset
	helmSettings *cli.EnvSettings
	valuesMapper func(swr onyxia.ServiceWithRelease) (map[string]any, error)
	result       *pkg.ServiceWithReleaseActionResult
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, catalogs onyxia.Catalogs, valuesMapper func(swr onyxia.ServiceWithRelease) (map[string]any, error)) *valueSetter {
	return &valueSetter{
		kubernetes:   client,
		helmSettings: settings,
		catalogs:     catalogs,
		valuesMapper: valuesMapper,
		result: &pkg.ServiceWithReleaseActionResult{
			FailedItems:     make([]string, 0),
			FailedItemType:  pkg.Release,
			SuccessfulCount: 0,
		},
	}
}

func (s *valueSetter) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	err := s.process(ctx, swr)
	if err != nil {
		s.result.FailedItems = append(s.result.FailedItems, swr.Release.Name)
		return err
	}
	s.result.SuccessfulCount++
	return nil
}

// Internal function such that we can wrap the FailedItems instead of having it in each err check
func (s *valueSetter) process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())
	actionConfig := new(action.Configuration)
	s.helmSettings.SetNamespace(swr.Release.Namespace)
	if err := actionConfig.Init(s.helmSettings.RESTClientGetter(), swr.Release.Namespace, ""); err != nil {
		log.Error("error initializing config for setvalues", "err", err)
		return err
	}
	log.Debug("attempting to setvalues")

	values, err := s.valuesMapper(swr)
	if err != nil {
		slog.Error("error mapping values to set", "err", err)
		return err
	}
	slog.Debug("Computed values with mapper", "values", values)

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
	upgradeAction.ForceConflicts = true
	upgradeAction.ServerSideApply = "true"

	_, err = upgradeAction.Run(swr.Release.Name, chart, values)
	if err != nil {
		log.Error("error upgrading release", "err", err)
		return err
	}

	log.Info("successfully setvalues")
	return nil
}

func (s *valueSetter) Finish(ctx context.Context) (*pkg.ServiceWithReleaseActionResult, error) {
	return s.result, nil
}
