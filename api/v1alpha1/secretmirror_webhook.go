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

package v1alpha1

import (
	"errors"
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

//+kubebuilder:webhook:path=/mutate-mirrors-kts-studio-v1alpha1-secretmirror,mutating=true,failurePolicy=fail,sideEffects=None,groups=mirrors.kts.studio,resources=secretmirrors,verbs=create;update,versions=v1alpha1,name=msecretmirror.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &SecretMirror{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *SecretMirror) Default() {
	secretmirrorlog.Info("default", "name", r.Name)

	if r.Spec.PollPeriodSeconds == 0 {
		r.Spec.PollPeriodSeconds = 3 * 60 // 3 minutes
	}

	if r.Spec.Destination.Namespace == "" && r.Spec.Destination.NamespaceRegex == "" {
		// trying to use pull mode
		r.Spec.Destination.Namespace = r.Namespace
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-mirrors-kts-studio-v1alpha1-secretmirror,mutating=false,failurePolicy=fail,sideEffects=None,groups=mirrors.kts.studio,resources=secretmirrors,verbs=create;update,versions=v1alpha1,name=vsecretmirror.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &SecretMirror{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *SecretMirror) ValidateCreate() error {
	secretmirrorlog.Info("validate create", "name", r.Name)

	if r.Spec.Source.Name == "" {
		return errors.New("source name is required")
	}

	if r.Spec.Destination.Namespace == "" && r.Spec.Destination.NamespaceRegex == "" {
		return errors.New("destination is empty")
	}

	if r.Spec.Destination.NamespaceRegex != "" {
		_, err := regexp.Compile(r.Spec.Destination.NamespaceRegex)
		return err
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
