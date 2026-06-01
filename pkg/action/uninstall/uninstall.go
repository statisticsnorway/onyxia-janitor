package uninstall

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/client-go/kubernetes"
)

type ServiceUninstaller struct {
	kubernetes   *kubernetes.Clientset
	helmSettings *cli.EnvSettings
	dryRun       bool
	result       *pkg.ServiceWithReleaseActionResult
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, dryRun bool) *ServiceUninstaller {
	return &ServiceUninstaller{
		kubernetes:   client,
		helmSettings: settings,
		dryRun:       dryRun,
		result: &pkg.ServiceWithReleaseActionResult{
			FailedItems:     make([]string, 0),
			FailedItemsType: pkg.Release,
			SuccessfulCount: 0,
		},
	}
}

// Process uninstalls the given service' Helm release if it has failed
// In the future this will probably just uninstall without any checks,
// as the checks will be in the filters before the action is run.
func (u *ServiceUninstaller) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	err := u.process(ctx, swr)
	if err != nil {
		u.result.FailedItems = append(u.result.FailedItems, swr.Release.Name)
		return err
	}
	u.result.SuccessfulCount++
	return nil
}

// Internal function such that we can wrap the FailedItems instead of having it in each err check
func (u *ServiceUninstaller) process(_ context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())
	actionConfig := new(action.Configuration)
	u.helmSettings.SetNamespace(swr.Release.Namespace)
	if err := actionConfig.Init(u.helmSettings.RESTClientGetter(), swr.Release.Namespace, ""); err != nil {
		log.Error("error initializing config for release uninstall", "err", err)
		return err
	}
	uninstallAction := action.NewUninstall(actionConfig)
	uninstallAction.WaitStrategy = kube.HookOnlyStrategy
	uninstallAction.DryRun = u.dryRun
	if u.dryRun {
		log.Info("running uninstall action with dry run")
	}

	slog.Debug("Running uninstall action", "action", uninstallAction)
	_, err := uninstallAction.Run(swr.Release.Name)
	if err != nil {
		log.Error("error uninstalling release", "err", err)
		return err
	}
	log.Info("successfully uninstalled release")
	return nil
}

func (u *ServiceUninstaller) Finish(_ context.Context) (*pkg.ServiceWithReleaseActionResult, error) {
	return u.result, nil
}
