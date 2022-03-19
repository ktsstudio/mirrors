/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var secretmirrorlog = logf.Log.WithName("secretmirror-resource")

func (r *SecretMirror) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-mirrors-kts-studio-v1alpha2-secretmirror,mutating=true,failurePolicy=fail,sideEffects=None,groups=mirrors.kts.studio,resources=secretmirrors,verbs=create;update,versions=v1alpha2,name=msecretmirror.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &SecretMirror{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *SecretMirror) Default() {
	if r.Spec.PollPeriodSeconds == 0 {
		r.Spec.PollPeriodSeconds = 3 * 60 // 3 minutes
	}

	if r.Spec.Source.Type == "" {
		r.Spec.Source.Type = SourceTypeSecret
	}

	if r.Spec.Source.Name == "" {
		r.Spec.Source.Name = r.Name
	}

	if r.Spec.Destination.Type == "" {
		r.Spec.Destination.Type = DestTypeNamespaces
	}

	if r.Spec.DeletePolicy == "" {
		r.Spec.DeletePolicy = DeletePolicyDelete
	}

	if r.Spec.Destination.Type == DestTypeVault {
		r.Spec.Destination.Vault.Default()
		r.Spec.DeletePolicy = DeletePolicyRetain
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-mirrors-kts-studio-v1alpha2-secretmirror,mutating=false,failurePolicy=fail,sideEffects=None,groups=mirrors.kts.studio,resources=secretmirrors,verbs=create;update,versions=v1alpha2,name=vsecretmirror.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &SecretMirror{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *SecretMirror) ValidateCreate() error {
	secretmirrorlog.Info("validate create", "name", r.Name)

	if r.Spec.Destination.Type == "" {
		return errors.New("destination type must be one of the following: `namespaces`, `vault")
	}

	if r.Spec.Source.Name == "" {
		return errors.New("source name is required")
	}

	if r.Spec.Destination.Type == DestTypeNamespaces {
		if len(r.Spec.Destination.Namespaces) == 0 {
			return errors.New("destination namespaces are empty")
		} else {
			for i, nsRegex := range r.Spec.Destination.Namespaces {
				if nsRegex == "" {
					return fmt.Errorf("destination namespace #%d is empty", i)
				}
				_, err := regexp.Compile(nsRegex)
				if err != nil {
					return fmt.Errorf("destination namespace #%d has a problem compiling: %s", i, err)
				}
			}
		}
	}

	if r.Spec.Destination.Type == DestTypeVault {
		if err := r.Spec.Destination.Vault.Validate(); err != nil {
			return err
		}
	}

	if r.Spec.DeletePolicy != "" && r.Spec.DeletePolicy != DeletePolicyDelete && r.Spec.DeletePolicy != DeletePolicyRetain {
		return errors.New("deletePolicy must be one of the following: `delete`, `retain`")
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *SecretMirror) ValidateUpdate(old runtime.Object) error {
	secretmirrorlog.Info("validate update", "name", r.Name)

	return r.ValidateCreate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *SecretMirror) ValidateDelete() error {
	secretmirrorlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
