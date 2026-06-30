package setonyxiasecret

import (
	"context"
	"log/slog"
	"onyxia-janitor/pkg"
	"onyxia-janitor/pkg/onyxia"

	"helm.sh/helm/v4/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const onyxiaSecretPrefix = "sh.onyxia.release.v1."

type OnxyiaSecretSetter struct {
	catalogs     onyxia.Catalogs
	kubernetes   *kubernetes.Clientset
	dryRun       bool
	secretMapper func(swr onyxia.ServiceWithRelease) (map[string]string, error)
	result       *pkg.ServiceWithReleaseActionResult
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, dryRun bool, catalogs onyxia.Catalogs, secretMapper func(swr onyxia.ServiceWithRelease) (map[string]string, error)) *OnxyiaSecretSetter {
	return &OnxyiaSecretSetter{
		kubernetes:   client,
		dryRun:       dryRun,
		catalogs:     catalogs,
		secretMapper: secretMapper,
		result: &pkg.ServiceWithReleaseActionResult{
			FailedItems:     make([]string, 0),
			FailedItemsType: pkg.Release,
			SuccessfulCount: 0,
		},
	}
}

func (s *OnxyiaSecretSetter) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	err := s.process(ctx, swr)
	if err != nil {
		s.result.FailedItems = append(s.result.FailedItems, swr.Release.Name)
		return err
	}
	s.result.SuccessfulCount++
	return nil
}

// Internal function such that we can wrap the FailedItems instead of having it in each err check
func (s *OnxyiaSecretSetter) process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())
	log.Debug("attempting to setonyxiasecret")

	values, err := s.secretMapper(swr)
	if err != nil {
		slog.Error("error mapping secret values to set", "err", err)
		return err
	}
	slog.Debug("Computed values with mapper", "values", values)

	secretsClient := s.kubernetes.CoreV1().Secrets(swr.Service.Namespace)

	if _, err := secretsClient.Apply(ctx,
		&v1.SecretApplyConfiguration{
			ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{Name: new(onyxiaSecretPrefix + swr.Service.Name)},
			StringData:                   values,
		},
		metav1.ApplyOptions{
			FieldManager: "onyxia-janitor",
		}); err != nil {
		slog.Error("could not update onyxia secret", "err", err)
	}

	log.Info("successfully setvalues")
	return nil
}

func (s *OnxyiaSecretSetter) Finish(_ context.Context) (*pkg.ServiceWithReleaseActionResult, error) {
	return s.result, nil
}
