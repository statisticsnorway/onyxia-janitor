package onyxia

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	release "helm.sh/helm/v4/pkg/release/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const onyxiaSecretType = "onyxia.sh/release.v1"
const onyxiaSecretPrefix = "sh.onyxia.release.v1."
const secretCatalogKey = "catalog"
const secretFriendlyNameKey = "friendlyName"

type client struct {
	lister SecretLister
}

type SecretLister interface {
	List(context.Context, v1.ListOptions) (*corev1.SecretList, error)
}

type Service struct {
	Name         string
	Namespace    string
	Catalog      string
	FriendlyName string
}

// This struct is avaiable in expr. Fields can be accessed via .Service.[...] or .Release.[...]
type ServiceWithRelease struct {
	Service Service
	Release release.Release
}

// AddToLogger adds some context to the given slog.Logger
func (swr ServiceWithRelease) AddToLogger(log *slog.Logger) *slog.Logger {
	return log.With(
		slog.Group("service",
			slog.String("friendlyName", swr.Service.FriendlyName),
			slog.String("name", swr.Service.Name),
			slog.String("catalog", swr.Service.Catalog),
		),
		slog.Group("release",
			slog.String("name", swr.Release.Name),
			slog.String("namespace", swr.Release.Namespace)),
		slog.Group("chart",
			slog.String("name", swr.Release.Chart.Metadata.Name),
			slog.String("version", swr.Release.Chart.Metadata.Version),
		),
	)
}

func New(lister SecretLister) *client {
	return &client{
		lister: lister,
	}
}

// List lists all Onyxia secrets that exist in the cluster.
func (c *client) List(ctx context.Context) ([]Service, error) {
	var services []Service
	cont := ""
	for {
		secrets, err := c.lister.List(ctx, v1.ListOptions{
			Continue:      cont,
			FieldSelector: fmt.Sprintf("type=%s", onyxiaSecretType),
		})
		if err != nil {
			return nil, err
		}

		for _, secret := range secrets.Items {
			service, err := parseServiceSecret(secret)
			if err != nil {
				return nil, err
			}
			services = append(services, service)
		}

		if cont == "" {
			break
		}
	}
	return services, nil
}

// ListConcurrent lists all Onyxia secrets present in the cluster concurrenctly
// through the given channel.
func (c *client) ListConcurrent(ctx context.Context, out chan Service) {
	defer close(out)
	cont := ""
	for {
		secrets, err := c.lister.List(ctx, v1.ListOptions{
			Continue:      cont,
			FieldSelector: fmt.Sprintf("type=%s", onyxiaSecretType),
		})
		if err != nil {
			slog.Error("list concurrent", "err", err.Error())
			return
		}

		for _, secret := range secrets.Items {
			service, err := parseServiceSecret(secret)
			if err != nil {
				slog.Error("parse service", "err", err.Error())
				return
			}
			select {
			case out <- service:
			case <-ctx.Done():
				return
			}
		}

		if cont == "" {
			break
		}
	}
}

// parseServiceSecret parses the raw Kubernetes secret into a Service object.
func parseServiceSecret(secret corev1.Secret) (Service, error) {
	name := strings.TrimPrefix(secret.Name, onyxiaSecretPrefix)

	catalog, ok := secret.Data[secretCatalogKey]
	if !ok {
		return Service{}, errors.New("missing catalog info")
	}

	friendlyName, ok := secret.Data[secretFriendlyNameKey]
	if !ok {
		slog.Error("friendly name not found", "data", secret.Data)
		friendlyName = []byte(name)
	}

	return Service{
		Name:         name,
		Namespace:    secret.Namespace,
		FriendlyName: string(friendlyName),
		Catalog:      string(catalog),
	}, nil
}
