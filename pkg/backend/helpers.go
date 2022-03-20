package backend

import (
	"bytes"
	v1 "k8s.io/api/core/v1"
)

func dataDiffer(src, dest map[string][]byte) bool {
	if len(src) != len(dest) {
		return true
	}

	for k := range src {
		if bytes.Compare(src[k], dest[k]) != 0 {
			return true
		}
	}
	return false
}

func secretDiffer(src, dest *v1.Secret) bool {
	for k := range src.Labels {
		if src.Labels[k] != dest.Labels[k] {
			return true
		}
	}

	for k := range src.Annotations {
		if src.Annotations[k] != dest.Annotations[k] {
			return true
		}
	}

	if dataDiffer(src.Data, dest.Data) {
		return true
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
