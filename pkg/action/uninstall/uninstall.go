package uninstall

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/kubernetes"
)

type serviceUninstaller struct {
	kubernetes   *kubernetes.Clientset
	helmSettings *cli.EnvSettings
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings) *serviceUninstaller {
	return &serviceUninstaller{
		kubernetes:   client,
		helmSettings: settings,
	}
}

// Process uninstalls the given service' Helm release if it has failed
// In the future this will probably just uninstall without any checks,
// as the checks will be in the filters before the action is run.
func (u *serviceUninstaller) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())
	actionConfig := new(action.Configuration)
	u.helmSettings.SetNamespace(swr.Release.Namespace)
	if err := actionConfig.Init(u.helmSettings.RESTClientGetter(), swr.Release.Namespace, "", log.Debug); err != nil {
		log.Error("error initializing config for release uninstall", "err", err)
		return err
	}
	if swr.Release.Info.Status != release.StatusFailed {
		return nil
	}
	uninstallAction := action.NewUninstall(actionConfig)
	_, err := uninstallAction.Run(swr.Release.Name)
	if err != nil {
		log.Error("error uninstalling release", "err", err)
		return err
	}
	log.Info("successfully uninstalled release")
	return nil
}

func (u *serviceUninstaller) Finish(ctx context.Context) error {
	return nil
}
