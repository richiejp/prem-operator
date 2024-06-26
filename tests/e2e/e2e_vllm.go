package e2e_test

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	api "github.com/premAI-io/prem-operator/api/v1alpha1"
	"github.com/premAI-io/prem-operator/controllers/constants"
	"github.com/premAI-io/prem-operator/controllers/resources"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("vllm test", func() {
	var artifactName string
	var deps, sds, pods dynamic.ResourceInterface
	var scheme *runtime.Scheme
	var artifact *api.AIDeployment
	var startTime time.Time

	custModel := []api.AIModel{{
		AIModelSpec: api.AIModelSpec{
			Uri: "https://huggingface.co/microsoft/phi-1_5/resolve/main/pytorch_model.bin?download=true",
		},
	}}

	JustBeforeEach(func() {
		startTime = time.Now()
		k8s := dynamic.NewForConfigOrDie(ctrl.GetConfigOrDie())
		scheme = runtime.NewScheme()
		err := api.AddToScheme(scheme)
		Expect(err).ToNot(HaveOccurred())

		sds = k8s.Resource(schema.GroupVersionResource{Group: api.GroupVersion.Group, Version: api.GroupVersion.Version, Resource: "aideployments"}).Namespace("default")
		pods = k8s.Resource(schema.GroupVersionResource{Group: corev1.GroupName, Version: corev1.SchemeGroupVersion.Version, Resource: "pods"}).Namespace("default")
		deps = k8s.Resource(schema.GroupVersionResource{Group: appsv1.GroupName, Version: appsv1.SchemeGroupVersion.Version, Resource: "deployments"}).Namespace("default")

		uArtifact := unstructured.Unstructured{}
		uArtifact.Object, _ = runtime.DefaultUnstructuredConverter.ToUnstructured(artifact)
		resp, err := sds.Create(context.TODO(), &uArtifact, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		artifactName = resp.GetName()
		GinkgoWriter.Printf("artifactName: %s\n", artifactName)
	})

	AfterEach(func() {
		err := sds.Delete(context.Background(), artifactName, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		checkLogs(startTime)
	})

	When("the config is good", func() {
		BeforeEach(func() {
			artifact = &api.AIDeployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "AIDeployment",
					APIVersion: api.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "vllm-",
				},
				Spec: api.AIDeploymentSpec{
					Engine: api.AIEngine{
						Name: "vllm",
					},
					Endpoint: []api.Endpoint{{
						Domain: "foo.127.0.0.1.nip.io",
					}},
					Models: custModel,
				},
			}
		})

		It("creates the deployment", func() {
			By("starting the workload with the associated label")
			Eventually(func(g Gomega) bool {
				deploymentPod := &corev1.Pod{}
				if !getObjectWithLabel(pods, deploymentPod, resources.DefaultAnnotation, artifactName) {
					return false
				}

				c := deploymentPod.Spec.Containers[0]
				g.Expect(c.Name).To(HavePrefix(constants.ContainerEngineName))
				g.Expect(c.StartupProbe).ToNot(BeNil())
				g.Expect(c.StartupProbe.InitialDelaySeconds).To(Equal(int32(3)))
				g.Expect(c.StartupProbe.PeriodSeconds).To(Equal(int32(1)))
				g.Expect(c.StartupProbe.FailureThreshold).To(Equal(int32(120)))

				g.Expect(c.ReadinessProbe).ToNot(BeNil())
				g.Expect(c.ReadinessProbe.FailureThreshold).To(Equal(int32(3)))

				g.Expect(c.LivenessProbe).ToNot(BeNil())
				g.Expect(c.LivenessProbe.PeriodSeconds).To(Equal(int32(30)))
				g.Expect(c.LivenessProbe.TimeoutSeconds).To(Equal(int32(15)))
				g.Expect(c.LivenessProbe.FailureThreshold).To(Equal(int32(10)))

				g.Expect(c.Resources.Requests["memory"]).To(Equal(resource.Quantity{}))
				g.Expect(c.Resources.Requests["cpu"]).To(Equal(resource.Quantity{}))
				g.Expect(c.Resources.Limits["memory"]).To(Equal(resource.Quantity{}))
				g.Expect(c.Resources.Limits["cpu"]).To(Equal(resource.Quantity{}))

				return true
			}).WithPolling(5 * time.Second).WithTimeout(time.Minute).Should(BeTrue())
		})

		When("we override the probe values", func() {
			initialDelay := int32(66)

			BeforeEach(func() {
				artifact = &api.AIDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "AIDeployment",
						APIVersion: api.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "vllm-",
					},
					Spec: api.AIDeploymentSpec{
						Engine: api.AIEngine{
							Name: "vllm",
						},
						Endpoint: []api.Endpoint{{
							Domain: "foo.127.0.0.1.nip.io",
						},
						},
						Models: custModel,
						Deployment: api.Deployment{
							StartupProbe: &api.Probe{
								InitialDelaySeconds: &initialDelay,
								PeriodSeconds:       33,
								TimeoutSeconds:      12,
								FailureThreshold:    13,
							},
							ReadinessProbe: &api.Probe{
								SuccessThreshold: 14,
							},
							LivenessProbe: &api.Probe{
								PeriodSeconds: 21,
							},
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									"memory": resource.MustParse("70Mi"),
								},
							},
						},
					},
				}
			})

			It("starts the API with the merged probe values", func() {
				By("starting the workload with the associated label")
				Eventually(func(g Gomega) bool {
					deploymentPod := &corev1.Pod{}
					if !getObjectWithLabel(pods, deploymentPod, resources.DefaultAnnotation, artifactName) {
						return false
					}

					c := deploymentPod.Spec.Containers[0]
					g.Expect(c.Name).To(HavePrefix(constants.ContainerEngineName))
					g.Expect(c.StartupProbe).ToNot(BeNil())
					g.Expect(c.StartupProbe.InitialDelaySeconds).To(Equal(int32(66)))
					g.Expect(c.StartupProbe.PeriodSeconds).To(Equal(int32(33)))
					g.Expect(c.StartupProbe.TimeoutSeconds).To(Equal(int32(12)))
					g.Expect(c.StartupProbe.FailureThreshold).To(Equal(int32(13)))

					g.Expect(c.ReadinessProbe).ToNot(BeNil())
					g.Expect(c.ReadinessProbe.FailureThreshold).To(Equal(int32(3)))
					g.Expect(c.ReadinessProbe.SuccessThreshold).To(Equal(int32(14)))

					g.Expect(c.LivenessProbe).ToNot(BeNil())
					g.Expect(c.LivenessProbe.PeriodSeconds).To(Equal(int32(21)))
					g.Expect(c.LivenessProbe.TimeoutSeconds).To(Equal(int32(15)))
					g.Expect(c.LivenessProbe.FailureThreshold).To(Equal(int32(10)))

					return true
				}).WithPolling(5 * time.Second).WithTimeout(time.Minute).Should(BeTrue())
			})
		})

		When("We specify a GPU", func() {
			BeforeEach(func() {
				artifact = &api.AIDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "AIDeployment",
						APIVersion: api.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "vllm-",
					},
					Spec: api.AIDeploymentSpec{
						Engine: api.AIEngine{
							Name: "vllm",
						},
						Endpoint: []api.Endpoint{{
							Domain: "foo.127.0.0.1.nip.io",
						},
						},
						Models: custModel,
						Deployment: api.Deployment{
							Accelerator: &api.Accelerator{
								Interface: api.AcceleratorInterfaceCUDA,
								MinVersion: &api.Version{
									Major: 7,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									"memory": resource.MustParse("200Gi"),
								},
							},
						},
					},
				}
			})

			It("Creates a deployment with the correct GPU count", func() {
				By("creating the workload with the associated label")
				Eventually(func(g Gomega) bool {
					deployment := &appsv1.Deployment{}
					if !getObjectWithName(deps, deployment, artifactName) {
						return false
					}

					nvidia := "nvidia"
					g.Expect(deployment.Spec.Template.Spec.RuntimeClassName).To(Equal(&nvidia))

					c := deployment.Spec.Template.Spec.Containers[0]
					g.Expect(c.Name).To(HavePrefix(constants.ContainerEngineName))
					g.Expect(c.Resources.Requests["memory"]).To(Equal(resource.MustParse("200Gi")))
					g.Expect(c.Resources.Requests[constants.NvidiaGPULabel]).To(Equal(resource.MustParse("1")))
					g.Expect(c.Resources.Limits[constants.NvidiaGPULabel]).To(Equal(resource.MustParse("1")))

					return true
				}).WithPolling(5 * time.Second).WithTimeout(time.Minute).Should(BeTrue())
			})
		})

		When("We specify AWQ in engine options", func() {
			BeforeEach(func() {
				artifact = &api.AIDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "AIDeployment",
						APIVersion: api.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "vllm-",
					},
					Spec: api.AIDeploymentSpec{
						Engine: api.AIEngine{
							Name: "vllm",
							Options: map[string]string{
								constants.DtypeKey:        "float16",
								constants.QuantizationKey: "awq",
							},
						},
						Endpoint: []api.Endpoint{{
							Domain: "foo.127.0.0.1.nip.io",
						}},
						Models: []api.AIModel{{
							AIModelSpec: api.AIModelSpec{
								Uri:          "TheBloke/TinyLlama-1.1B-Chat-v1.0-AWQ",
								Quantization: api.AIModelQuantizationAWQ,
								DataType:     api.AIModelDataTypeFloat16,
							},
						}},
						Deployment: api.Deployment{
							Accelerator: &api.Accelerator{
								Interface: api.AcceleratorInterfaceCUDA,
								MinVersion: &api.Version{
									Major: 8,
								},
							},
						},
					},
				}
			})

			It("Creates an engine with the correct arguments", func() {
				By("creating the workload with the associated label")
				Eventually(func(g Gomega) bool {
					deployment := &appsv1.Deployment{}
					if !getObjectWithName(deps, deployment, artifactName) {
						return false
					}

					c := deployment.Spec.Template.Spec.Containers[0]
					g.Expect(c.Name).To(HavePrefix(constants.ContainerEngineName))
					g.Expect(c.Args).To(ContainElements("--dtype", "float16"))
					g.Expect(c.Args).To(ContainElements("--quantization", "awq"))

					return true
				}).WithPolling(5 * time.Second).WithTimeout(time.Minute).Should(BeTrue())
			})
		})

		When("we reference a model CRD", func() {
			var modelMap *api.AIModelMap

			BeforeEach(func() {
				modelMap = createModelMapSingleEntry(api.AIEngineNameVLLM, "original", api.AIModelSpec{
					Uri: "microsoft/phi-2",
				})

				artifact = &api.AIDeployment{
					TypeMeta: metav1.TypeMeta{
						Kind:       "AIDeployment",
						APIVersion: api.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: string(api.AIEngineNameVLLM) + "-",
					},
					Spec: api.AIDeploymentSpec{
						Engine: api.AIEngine{
							Name: api.AIEngineNameVLLM,
						},
						Endpoint: []api.Endpoint{{
							Domain: "foo.127.0.0.1.nip.io",
						}},
						Models: []api.AIModel{
							{
								ModelMapRef: &api.AIModelMapReference{
									Name:    modelMap.Name,
									Variant: "original",
								},
							},
						},
					},
				}
			})

			It("starts the API with the correct args", func() {
				By("starting the workload")
				Eventually(func(g Gomega) bool {
					deploymentPod := &corev1.Pod{}
					if !getObjectWithLabel(pods, deploymentPod, resources.DefaultAnnotation, artifactName) {
						return false
					}

					c := deploymentPod.Spec.Containers[0]
					g.Expect(c.Args).To(Equal([]string{"--model", "microsoft/phi-2"}))
					return true
				}).WithPolling(5 * time.Second).WithTimeout(time.Minute).Should(BeTrue())
			})

			AfterEach(func() {
				c, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
				Expect(err).ToNot(HaveOccurred())
				err = c.Delete(context.Background(), modelMap)
				Expect(err).ToNot(HaveOccurred())
			})

		})
	})
})
