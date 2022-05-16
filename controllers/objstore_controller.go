/*
Copyright 2022.

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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cninfv1alpha1 "github.com/aviadsTraiana/cninf-operator/api/v1alpha1"
)

// ObjStoreReconciler reconciles a ObjStore object
type ObjStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ObjStore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *ObjStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	instance := &cninfv1alpha1.ObjStore{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		//if not found we will just skip
		l.Error(err, "unable to get resource") // not for production code
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if instance.Status.State == "" {
		instance.Status.State = cninfv1alpha1.PENDING_STATE
		err := r.Status().Update(ctx, instance) //updting the status, not the object
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObjStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cninfv1alpha1.ObjStore{}).
		Complete(r)
}
