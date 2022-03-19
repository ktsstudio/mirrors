package v1alpha1

import (
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

func (r *SecretMirror) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.SecretMirror)

	dst.ObjectMeta = r.ObjectMeta

	dst.Spec.Source.Type = v1alpha2.SourceTypeSecret
	dst.Spec.Source.Name = r.Spec.Source.Name

	dst.Spec.Destination.Type = v1alpha2.DestTypeNamespaces
	dst.Spec.Destination.Namespaces = []string{r.Spec.Destination.NamespaceRegex}

	dst.Spec.PollPeriodSeconds = r.Spec.PollPeriodSeconds

	dst.Status.MirrorStatus = v1alpha2.MirrorStatus(r.Status.MirrorStatus)
	dst.Status.LastSyncTime = r.Status.LastSyncTime

	dst.Default()
	return nil
}

func (r *SecretMirror) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.SecretMirror)

	r.ObjectMeta = src.ObjectMeta

	r.Spec.Source.Name = src.Spec.Source.Name
	r.Spec.PollPeriodSeconds = src.Spec.PollPeriodSeconds

	r.Status.MirrorStatus = MirrorStatus(src.Status.MirrorStatus)
	r.Status.LastSyncTime = src.Status.LastSyncTime

	if src.Spec.Source.Type != v1alpha2.SourceTypeSecret || src.Spec.Destination.Type != v1alpha2.DestTypeNamespaces {
		return nil
	}

	if len(src.Spec.Destination.Namespaces) > 0 {
		r.Spec.Destination.NamespaceRegex = src.Spec.Destination.Namespaces[0]
	} else {
		r.Spec.Destination.NamespaceRegex = ""
	}

	return nil
}
