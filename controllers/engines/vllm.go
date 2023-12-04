package engines

import (
	"fmt"
	a1 "github.com/premAI-io/saas-controller/api/v1alpha1"
	"github.com/premAI-io/saas-controller/controllers/aideployment"
	"github.com/premAI-io/saas-controller/controllers/resources"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	VllmAiEngine            = "vllm"
	vllmContainerVolumePath = "/root/.cache/huggingface"
)

var (
	vllmImageRepo   = "vllm/vllm-openai"
	vllmImageFormat = "%s:%s"
)

var (
	ErrModelsNotSpecified = fmt.Errorf("models not specified")
	ErrorOnlyOneModel     = fmt.Errorf("only one model can be specified")
)

type vllmAi struct {
	// image of the vllm engine
	engineImage string
	// name of the vllm llm model to use
	llmName string
	// environment variables to pass to the vllm engine
	engineEnvVars []v1.EnvVar

	// name of the k8s resource
	resourceName string
	// k8s namespace
	namespace string
	// name of the container
	containerName string

	// used to customize the deployment
	deploymentOptions a1.Deployment
}

func NewVllmAi(ai *a1.AIDeployment) (aideployment.MLEngine, error) {
	if len(ai.Spec.Models) == 0 {
		return nil, ErrModelsNotSpecified
	}

	if len(ai.Spec.Models) > 1 {
		return nil, ErrorOnlyOneModel
	}

	model := ai.Spec.Models[0]
	imageTag := "latest"
	imageRepo := vllmImageRepo
	if ai.Spec.Engine.Options["imageTag"] != "" {
		imageTag = ai.Spec.Engine.Options["imageTag"]
	}
	if ai.Spec.Engine.Options["imageRepo"] != "" {
		imageRepo = ai.Spec.Engine.Options["imageRepo"]
	}
	engineImage := fmt.Sprintf(vllmImageFormat, imageRepo, imageTag)

	return &vllmAi{
		engineImage: engineImage,
		llmName:     model.Custom.Url,

		resourceName:      ai.Name,
		namespace:         ai.Namespace,
		engineEnvVars:     ai.Spec.Env,
		containerName:     model.Custom.Name,
		deploymentOptions: ai.Spec.Deployment,
	}, nil
}

func (v *vllmAi) Port() int32 {
	return 8000
}

func (v *vllmAi) Deployment(owner metav1.Object) (*appsv1.Deployment, error) {
	log.Info("Creating deployment for vllm engine, model: ", v.llmName)
	container := v1.Container{
		ImagePullPolicy: v1.PullAlways,
		Name:            v.containerName,
		Image:           v.engineImage,
		Env:             v.engineEnvVars,
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "models",
				MountPath: vllmContainerVolumePath,
			},
		},
		Args: []string{
			"--model", v.llmName,
		},
		Resources: v1.ResourceRequirements{
			Limits: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			Requests: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
		},
	}

	serviceAccount := false
	runtimeClassName := "nvidia"
	replicas := int32(1)
	if v.deploymentOptions.Replicas != nil {
		replicas = *v.deploymentOptions.Replicas
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            v.resourceName,
			Namespace:       v.namespace,
			OwnerReferences: resources.GenOwner(owner),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: resources.GenDefaultLabels(v.resourceName),
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: mergeMaps(
						resources.GenDefaultLabels(v.resourceName),
						v.deploymentOptions.Labels,
					),
					Annotations: mergeMaps(
						map[string]string{},
						v.deploymentOptions.Annotations,
					),
				},
				Spec: v1.PodSpec{
					Containers:                   []v1.Container{container},
					AutomountServiceAccountToken: &serviceAccount,
					RuntimeClassName:             &runtimeClassName,
					Volumes: []v1.Volume{
						{
							Name: "models",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	return deployment, nil
}

func mergeMaps(a, b map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
	}
	return result
}
