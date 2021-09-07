package backend

import (
	"bytes"
	"errors"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	v1 "k8s.io/api/core/v1"
	"regexp"
)

var (
	errNoNamespaces = errors.New("no namespaces found")
)

func secretDiffer(src, dest *v1.Secret) bool {
	if len(src.Labels) != len(dest.Labels) {
		return true
	}

	for k := range src.Labels {
		if src.Labels[k] != dest.Labels[k] {
			return true
		}
	}

	if len(src.Data) != len(dest.Data) {
		return true
	}

	for k := range src.Data {
		if bytes.Compare(src.Data[k], dest.Data[k]) != 0 {
			return true
		}
	}
	return false
}

func copySecret(src, dest *v1.Secret) {
	for k, v := range src.Labels {
		dest.Labels[k] = v
	}
	for k, v := range src.Annotations {
		dest.Annotations[k] = v
	}
	dest.Data = make(map[string][]byte)
	for k, v := range src.Data {
		dataCopy := make([]byte, len(v))
		copy(dataCopy, v)
		dest.Data[k] = dataCopy
	}
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func getFilteredNamespaces(keeper *nskeeper.NSKeeper, regex *regexp.Regexp) ([]string, error) {
	namespacesList := keeper.GetNamespaces()
	if namespacesList == nil {
		return nil, errNoNamespaces
	}
	return filterNamespacesByRegex(namespacesList.Items, regex), nil
}

func filterNamespacesByRegex(namespaces []v1.Namespace, regex *regexp.Regexp) []string {
	result := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		if regex.MatchString(ns.Name) {
			result = append(result, ns.Name)
		}
	}
	return result
}
