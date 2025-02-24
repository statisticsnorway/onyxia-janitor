package onyxia

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func New(lister SecretLister) *client {
	return &client{
		lister: lister,
	}
}

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
		Namespace:    secret.Namespace,
		FriendlyName: string(friendlyName),
		Catalog:      string(catalog),
	}, nil
}
