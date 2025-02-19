package namespaces

import (
	"context"

	"github.com/expr-lang/expr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1typed "k8s.io/client-go/kubernetes/typed/core/v1"
)

type getter struct {
	namespaces corev1typed.NamespaceInterface
	filters    []filterFunc
}

type filterFunc func(corev1.Namespace) bool

type optFunc func(*getter) error

func New(client corev1typed.NamespaceInterface, opts ...optFunc) (*getter, error) {
	g := &getter{
		namespaces: client,
	}

	for _, opt := range opts {
		if err := opt(g); err != nil {
			return nil, err
		}
	}

	return g, nil
}

func WithFilter(f filterFunc) optFunc {
	return func(g *getter) error {
		g.filters = append(g.filters, f)
		return nil
	}
}

func WithExprFilter(exprFilter string) optFunc {
	return func(g *getter) error {
		prg, err := expr.Compile(
			exprFilter,
			expr.Env(corev1.Namespace{}),
			expr.AsBool(),
			expr.WarnOnAny(),
		)
		if err != nil {
			return err
		}

		g.filters = append(g.filters, func(n corev1.Namespace) bool {
			// Should be safe to ignore error as the filter has been statically verified
			res, _ := expr.Run(prg, n)
			return res.(bool)
		})
		return nil
	}
}

// List returns a filtered list of namespaces.
func (g *getter) List(ctx context.Context) ([]corev1.Namespace, error) {
	var namespaces []corev1.Namespace
	cont := ""
	for {
		namespaceList, err := g.namespaces.List(ctx, v1.ListOptions{Continue: cont})
		if err != nil {
			return nil, err
		}

		for _, namespace := range namespaceList.Items {
			if g.filter(namespace) {
				namespaces = append(namespaces, namespace)
			}
		}

		cont = namespaceList.Continue
		if cont == "" {
			break
		}
	}
	return namespaces, nil
}

func (g *getter) filter(ns corev1.Namespace) bool {
	for _, filter := range g.filters {
		if !filter(ns) {
			return false
		}
	}
	return true
}
