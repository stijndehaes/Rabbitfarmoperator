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

package controllers

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	farmv1 "rabbitco.io/api/v1"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (_ RealClock) Now() time.Time { return time.Now() }

// RabbitReconciler reconciles a Rabbit object
type RabbitReconciler struct {
	Clock
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	lastPopulationIncreaseKey = "rabbit.farm.io/lastPopulationIncrease"
)

//+kubebuilder:rbac:groups=farm.rabbitco.io,resources=rabbits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=farm.rabbitco.io,resources=rabbits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=farm.rabbitco.io,resources=rabbits/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Rabbit object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *RabbitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("rabbit", req.NamespacedName)

	var rabbit farmv1.Rabbit
	if err := r.Get(ctx, req.NamespacedName, &rabbit); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		err = client.IgnoreNotFound(err)
		if err != nil {
			r.Log.Error(err, "unable to fetch rabbit")
		}
		return ctrl.Result{}, err
	}
	r.Log.Info("received update event", "name", rabbit.Name, "namespace", rabbit.Namespace)
	r.UpdateRabbits(&rabbit)
	if err := r.Status().Update(ctx, &rabbit); err != nil {
		return ctrl.Result{}, err
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rabbit.Name,
			Namespace: rabbit.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		labels := map[string]string{
			"RabbitFarm": rabbit.Name,
		}
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &rabbit.Status.Rabbits,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    "base",
							Image:   "busybox",
							Command: []string{"tail", "-f", "/dev/null"},
						},
					},
				},
			},
		}
		if err := ctrl.SetControllerReference(&rabbit, deployment, r.Scheme); err != nil {
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "Failed updating deployment")
		return ctrl.Result{}, err
	}
	if rabbit.Spec.IncreasePopulationSeconds != 0 {
		dur := rabbit.Status.LastPopulationIncrease.Add(time.Duration(rabbit.Spec.IncreasePopulationSeconds) * time.Second).Sub(r.Clock.Now())
		r.Log.Info("Scheduling for requeue in", "duration", dur)
		return ctrl.Result{
			RequeueAfter: dur,
		}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RabbitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Log.V(1).Info("No clock specified, creating realClock")
		r.Clock = &RealClock{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&farmv1.Rabbit{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *RabbitReconciler) UpdateRabbits(rabbit *farmv1.Rabbit) {
	if rabbit.Status.Rabbits == 0 {
		rabbit.Status.Rabbits = rabbit.Spec.StartingPopulation
		rabbit.Status.LastPopulationIncrease = metav1.NewTime(r.Clock.Now())
		return
	}
	if rabbit.Spec.IncreasePopulationSeconds == 0 {
		r.Log.V(1).Info("Increasing populations second zero nothing to do")
		return
	}
	var populationIncrease = calculatePopulationIncrease(r.Clock.Now(), rabbit.Status.LastPopulationIncrease, rabbit.Spec.IncreasePopulationSeconds)
	if populationIncrease > 0 {
		r.Log.V(1).Info("Increasing population", "old", rabbit.Status.Rabbits, "new", rabbit.Status.Rabbits+populationIncrease)
		rabbit.Status.Rabbits = rabbit.Status.Rabbits + populationIncrease
		rabbit.Status.LastPopulationIncrease = metav1.NewTime(r.Clock.Now())
	} else {
		r.Log.V(1).Info("Increase of population not needed yet", "lastIncrease", rabbit.Status.LastPopulationIncrease, "now", r.Clock.Now(), "population", rabbit.Status.Rabbits)
	}
}

func calculatePopulationIncrease(now time.Time, lastIncrease metav1.Time, increaseSeconds int32) int32 {
	if now.After(lastIncrease.Time) {
		return int32(now.Unix()-lastIncrease.Unix()) / increaseSeconds
	} else {
		return 0
	}
}
