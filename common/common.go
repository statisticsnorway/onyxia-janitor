package common

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetNamespacesWithPrefix(clientset *kubernetes.Clientset, prefix string) ([]string, error) {
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var matchedNamespaces []string
	for _, ns := range namespaces.Items {
		if strings.HasPrefix(ns.Name, prefix) {
			matchedNamespaces = append(matchedNamespaces, ns.Name)
		}
	}
	return matchedNamespaces, nil
}

func IsAllowedChart(chartName string, allowedCharts []string) bool {
	for _, allowed := range allowedCharts {
		if allowed == chartName {
			return true
		}
	}
	return false
}
