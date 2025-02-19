package onyxia

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1typed "k8s.io/client-go/kubernetes/typed/core/v1"
)

const onyxiaSecretType = "onyxia.sh/release.v1"
const onyxiaSecretPrefix = "sh.onyxia.release.v1."
const secretCatalogKey = "catalog"
const secretFriendlyNameKey = "friendlyName"

type client struct {
	secrets corev1typed.SecretsGetter
}

type Service struct {
	Catalog      string
	FriendlyName string
	Name         string
}

func New(secrets corev1typed.SecretsGetter) *client {
	return &client{
		secrets: secrets,
	}
}

func (c *client) List(ctx context.Context, namespace string) ([]Service, error) {
	nsSecrets := c.secrets.Secrets(namespace)
	var services []Service
	cont := ""
	for {
		secrets, err := nsSecrets.List(ctx, v1.ListOptions{
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

func parseServiceSecret(secret corev1.Secret) (Service, error) {
	name := strings.TrimPrefix(secret.Name, onyxiaSecretPrefix)

	catalog, ok := secret.Data[secretCatalogKey]
	if !ok {
		return Service{}, errors.New("missing catalog info")
	}

	friendlyName, ok := secret.Data[secretFriendlyNameKey]
	if !ok {
		friendlyName = []byte(name)
	}

	return Service{
		Name:         name,
		FriendlyName: string(friendlyName),
		Catalog:      string(catalog),
	}, nil
}
