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
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	farmv1 "rabbitco.io/api/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var fakeClock *FakeClock

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

type FakeClock struct {
	CurrentTime time.Time
}

func (f *FakeClock) Now() time.Time { return f.CurrentTime }

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = farmv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())

	fakeClock = &FakeClock{
		CurrentTime: time.Now(),
	}
	r := &RabbitReconciler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Rabbit"),
		Scheme: scheme.Scheme,
		Clock:  fakeClock,
	}

	err = r.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

}, 60)

var interval = time.Millisecond * 250
var timeout = time.Second * 20
var duration = time.Second * 2

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Rabbit controller", func() {

	Context("Should manage rabbit population", func() {
		It("Should create the farm with the starting population", func() {
			By("Creating a new rabbit farm")
			ctx := context.Background()
			rabbit := createRabbitFarm(10, 1)

			Expect(k8sClient.Create(ctx, &rabbit)).Should(Succeed())

			By("The initial population status should be the starting population")
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)
		})

		It("Should increase population every x seconds", func() {
			By("Creating a new rabbit farm")
			ctx := context.Background()
			rabbit := createRabbitFarm(10, 1)
			Expect(k8sClient.Create(ctx, &rabbit)).Should(Succeed())

			By("The initial population status should remain the starting population")
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)

			Consistently(func() (int32, error) {
				rabbitLookupKey := types.NamespacedName{Name: rabbit.Name, Namespace: rabbit.Namespace}
				createdRabbit := &farmv1.Rabbit{}
				err := k8sClient.Get(ctx, rabbitLookupKey, createdRabbit)
				if err != nil {
					return -1, err
				}
				return createdRabbit.Status.Rabbits, nil
			}, duration, interval).Should(Equal(rabbit.Spec.StartingPopulation))

			By("Advancing the clock the population should increase")
			fakeClock.CurrentTime = fakeClock.CurrentTime.Add(time.Second)
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation+1)
		})

		It("Should not increase if 5 seconds are not passed", func() {
			By("Creating a new rabbit farm")
			ctx := context.Background()
			rabbit := createRabbitFarm(10, 5)
			Expect(k8sClient.Create(ctx, &rabbit)).Should(Succeed())

			By("The initial population status should be the starting population")
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)

			By("Advancing the clock the population should increase")
			fakeClock.CurrentTime = fakeClock.CurrentTime.Add(time.Second)
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)
		})

		It("Should increase population with 3 when service was down for 3 seconds", func() {
			By("Creating a new rabbit farm")
			ctx := context.Background()
			rabbit := createRabbitFarm(10, 1)
			Expect(k8sClient.Create(ctx, &rabbit)).Should(Succeed())

			By("The initial population status should be the starting population")
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)

			By("Advancing with 3 seconds, mimics downtime of 3 seconds ")
			fakeClock.CurrentTime = fakeClock.CurrentTime.Add(3 * time.Second)
			verifyPopulationEventuallyEquals(ctx, rabbit, 13)
		})

		It("Should increase population with 3 when service was down for 3,5 seconds", func() {
			By("Creating a new rabbit farm")
			ctx := context.Background()
			rabbit := createRabbitFarm(10, 1)
			Expect(k8sClient.Create(ctx, &rabbit)).Should(Succeed())

			By("The initial population status should be the starting population")
			verifyPopulationEventuallyEquals(ctx, rabbit, rabbit.Spec.StartingPopulation)

			By("Advancing by 3.5 seconds, mimicing downtime ")
			fakeClock.CurrentTime = fakeClock.CurrentTime.Add(3500 * time.Millisecond)
			verifyPopulationEventuallyEquals(ctx, rabbit, 13)
		})
	})
})

func verifyPopulationEventuallyEquals(ctx context.Context, rabbit farmv1.Rabbit, population int32) bool {
	rabbitLookupKey := types.NamespacedName{Name: rabbit.Name, Namespace: rabbit.Namespace}
	createdRabbit := &farmv1.Rabbit{}

	return Eventually(func() (int32, error) {
		err := k8sClient.Get(ctx, rabbitLookupKey, createdRabbit)
		if err != nil {
			return -1, err
		}
		return createdRabbit.Status.Rabbits, nil
	}, 1*timeout, interval).Should(Equal(population))
}

func createRabbitFarm(startingPopulation int32, increaseSeconds int32) farmv1.Rabbit {
	return farmv1.Rabbit{
		TypeMeta: metav1.TypeMeta{APIVersion: farmv1.GroupVersion.String(), Kind: "Rabbit"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.New().String(),
			Namespace: "default",
		},
		Spec: farmv1.RabbitSpec{
			StartingPopulation:        startingPopulation,
			IncreasePopulationSeconds: increaseSeconds,
		},
	}
}
