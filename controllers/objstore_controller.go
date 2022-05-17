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
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cninfv1alpha1 "github.com/aviadsTraiana/cninf-operator/api/v1alpha1"
)

const (
	configMapName = "%s-cm"
	finalizer     = "objstore.cninf.aviad.okro.com"
)

// ObjStoreReconciler reconciles a ObjStore object
type ObjStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	S3svc  *s3.S3 //client acess to aws s3 service
}

//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cninf.aviad.okro.com,resources=objstores/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;create;update;delete

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

	objStore := &cninfv1alpha1.ObjStore{}
	if err := r.Get(ctx, req.NamespacedName, objStore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if objStore.DeletionTimestamp.IsZero() { //before any delete command
		if objStore.Status.State == "" {
			objStore.Status.State = cninfv1alpha1.PENDING_STATE
			err := r.Status().Update(ctx, objStore) //updting the status, not the object
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		//add finalizer
		controllerutil.AddFinalizer(objStore, finalizer)
		if err := r.Update(ctx, objStore); err != nil {
			return ctrl.Result{}, err
		}
		if objStore.Status.State == cninfv1alpha1.PENDING_STATE {
			l.Info("starting to create bucket")
			if err := r.createResources(ctx, objStore); err != nil {
				objStore.Status.State = cninfv1alpha1.ERROR_STATE // we detected an error, no need to repeat logic
				l.Error(err, "error while creating bucket")
				err2 := r.Status().Update(ctx, objStore)
				l.Error(err2, "failed to update status")
				return ctrl.Result{}, err
			}
		}
	} else {
		l.Info("in deletion flow")
		if err := r.deleteResources(ctx, objStore); err != nil {
			l.Error(err, "failed to delete bucket")
			objStore.Status.State = cninfv1alpha1.ERROR_STATE
			err2 := r.Status().Update(ctx, objStore)
			if err2 != nil {
				l.Error(err2, "failed to update status")
			}
			return ctrl.Result{}, err
		}
		//remove finalizer to avoid hanging
		controllerutil.RemoveFinalizer(objStore, finalizer)
		if err := r.Update(ctx, objStore); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ObjStoreReconciler) createResources(ctx context.Context, objStore *cninfv1alpha1.ObjStore) error {
	//update the status to creating first
	objStore.Status.State = cninfv1alpha1.CREATING_STATE
	if err := r.Status().Update(ctx, objStore); err != nil {
		return err
	}
	//create the bucket
	bucket, err := r.createBucketBasedOnObjStore(objStore)
	if err != nil {
		return err
	}
	// after bucket got created, it is time to create the configmap resource
	err = r.createConfigMapBasedOnObjStore(ctx, objStore, bucket)
	if err != nil {
		return err
	}
	//update state of object store
	objStore.Status.State = cninfv1alpha1.CREATED_STATE
	err = r.Status().Update(ctx, objStore)
	if err != nil {
		return err
	}
	return nil
}

func (r *ObjStoreReconciler) createBucketBasedOnObjStore(objStore *cninfv1alpha1.ObjStore) (*s3.CreateBucketOutput, error) {
	awsBucketName := aws.String(objStore.Spec.Name)
	bucket, err := r.S3svc.CreateBucket(
		&s3.CreateBucketInput{
			Bucket:                     awsBucketName,
			ObjectLockEnabledForBucket: aws.Bool(objStore.Spec.Locked),
		})
	if err != nil {
		return nil, err
	}
	//await for creation
	err = r.S3svc.WaitUntilBucketExists(&s3.HeadBucketInput{Bucket: awsBucketName})
	if err != nil {
		return nil, err
	}
	return bucket, nil
}

func (r *ObjStoreReconciler) createConfigMapBasedOnObjStore(ctx context.Context, objStore *cninfv1alpha1.ObjStore, bucket *s3.CreateBucketOutput) error {
	data := make(map[string]string, 0)
	data["bucketName"] = objStore.Spec.Name
	data["location"] = *bucket.Location
	configmap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(configMapName, objStore.Spec.Name),
			Namespace: objStore.Namespace,
		},
		Data: data,
	}
	//create configmap on cluster
	return r.Create(ctx, configmap)
}

func (r *ObjStoreReconciler) deleteResources(ctx context.Context, objStore *cninfv1alpha1.ObjStore) error {
	//first we delete the bucket
	_, err := r.S3svc.DeleteBucket(&s3.DeleteBucketInput{Bucket: aws.String(objStore.Spec.Name)})
	if err != nil {
		return err
	}
	//right after (with no await) we delete the configmap
	configmap := &v1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKey{
		Name:      fmt.Sprintf(configMapName, objStore.Spec.Name),
		Namespace: objStore.Namespace,
	}, configmap)
	if err != nil {
		return err
	}
	err = r.Delete(ctx, configmap)
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObjStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cninfv1alpha1.ObjStore{}).
		Complete(r)
}
