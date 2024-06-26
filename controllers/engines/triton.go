package engines

import (
	"fmt"
	"strings"

	"github.com/premAI-io/prem-operator/controllers/aideployment"
	"github.com/premAI-io/prem-operator/controllers/aimodelmap"
	"github.com/premAI-io/prem-operator/controllers/constants"
	"github.com/premAI-io/prem-operator/pkg/utils"

	a1 "github.com/premAI-io/prem-operator/api/v1alpha1"
	"github.com/premAI-io/prem-operator/controllers/resources"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type Triton struct {
	AIDeployment *a1.AIDeployment
	Models       []aimodelmap.ResolvedModel
}

func NewTriton(ai *a1.AIDeployment, m []aimodelmap.ResolvedModel) aideployment.MLEngine {
	return &Triton{AIDeployment: ai, Models: m}

}

func (l *Triton) MetricsPort() int32 {
	return 8002
}

func (l *Triton) Port() int32 {
	return 8000
}

func (l *Triton) Deployment(owner metav1.Object) (*appsv1.Deployment, error) {
	objMeta := metav1.ObjectMeta{
		Name:            l.AIDeployment.Name,
		Namespace:       l.AIDeployment.Namespace,
		OwnerReferences: resources.GenOwner(owner),
	}

	imageTag := constants.ImageTagTritonDefault
	if l.AIDeployment.Spec.Engine.Options[constants.ImageTagKey] != "" {
		imageTag = l.AIDeployment.Spec.Engine.Options[constants.ImageTagKey]
	}

	imageRepository := constants.ImageRepositoryTriton
	if l.AIDeployment.Spec.Engine.Options[constants.ImageRepositoryKey] != "" {
		imageRepository = l.AIDeployment.Spec.Engine.Options[constants.ImageRepositoryKey]
	}

	deployment := appsv1.Deployment{}
	if l.AIDeployment.Spec.Deployment.PodTemplate != nil {
		deployment.Spec.Template = *l.AIDeployment.Spec.Deployment.PodTemplate.DeepCopy()
	} else {
		deployment.Spec.Template = v1.PodTemplateSpec{}
	}
	deployment.Spec.Replicas = l.AIDeployment.Spec.Deployment.Replicas
	pod := &deployment.Spec.Template.Spec

	serviceAccount := false

	image := fmt.Sprintf("%s:%s", imageRepository, imageTag)

	expose := &v1.Container{
		ImagePullPolicy: v1.PullAlways,
		Name:            constants.ContainerEngineName,
		Image:           image,
		Args: []string{
			"tritonserver",
			"--model-repository=/models",
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "models",
				MountPath: "/models",
			},
		},
		StartupProbe: &v1.Probe{
			PeriodSeconds:    10,
			FailureThreshold: 30,
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/v2/health/ready",
					Port: intstr.FromInt(int(l.Port())),
				},
			},
		},
		ReadinessProbe: &v1.Probe{
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/v2/health/ready",
					Port: intstr.FromInt(int(l.Port())),
				},
			},
		},
		LivenessProbe: &v1.Probe{
			InitialDelaySeconds: 15,
			PeriodSeconds:       10,
			TimeoutSeconds:      25,
			FailureThreshold:    3,
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/v2/health/live",
					Port: intstr.FromInt(int(l.Port())),
				},
			},
		},
	}

	mergeProbe(l.AIDeployment.Spec.Deployment.StartupProbe, expose.StartupProbe)
	mergeProbe(l.AIDeployment.Spec.Deployment.ReadinessProbe, expose.ReadinessProbe)
	mergeProbe(l.AIDeployment.Spec.Deployment.LivenessProbe, expose.LivenessProbe)

	pod.AutomountServiceAccountToken = &serviceAccount
	pod.Volumes = append(pod.Volumes, v1.Volume{
		Name: "models",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	})

	for _, m := range l.Models {
		// if the URL doesn't point to a tar file
		if strings.HasPrefix(m.Spec.Uri, "http") && !strings.Contains(m.Spec.Uri, ".tar") {
			pod.InitContainers = append(pod.InitContainers, v1.Container{
				ImagePullPolicy: v1.PullAlways,
				Name:            fmt.Sprintf("init-%s", m.Name),
				Image:           image,
				Command:         []string{"sh", "-c"},
				// needs to be in a single line as sh -c accepts a single input
				Args: []string{"curl --create-dirs -O --output-dir /models/$MODEL_NAME/1 $MODEL_PATH"},
				Env: []v1.EnvVar{
					{Name: "MODEL_NAME", Value: m.Name},
					{Name: "MODEL_PATH", Value: m.Spec.Uri},
				},
				VolumeMounts: []v1.VolumeMount{
					{
						Name:      "models",
						MountPath: "/models",
					},
				},
			})
		} else if strings.HasPrefix(m.Spec.Uri, "http") {
			pod.InitContainers = append(pod.InitContainers, v1.Container{
				ImagePullPolicy: v1.PullAlways,
				Name:            fmt.Sprintf("init-%s", m.Name),
				Image:           image,
				Command:         []string{"sh", "-c"},
				// needs to be in a single line as sh -c accepts a single input
				Args: []string{"curl -s -L $MODEL_PATH | tar xvz - -C /models"},
				Env: []v1.EnvVar{
					{Name: "MODEL_NAME", Value: m.Name},
					{Name: "MODEL_PATH", Value: m.Spec.Uri},
				},
				VolumeMounts: []v1.VolumeMount{
					{
						Name:      "models",
						MountPath: "/models",
					},
				},
			})
		} else {
			return nil, fmt.Errorf("invalid model URI, requires valid model url with \"http\" for downloading models")
		}
	}

	pod.Containers = append(pod.Containers, *expose)
	deploymentLabels := resources.GenDefaultLabels(l.AIDeployment.Name)
	deployment.Spec.Template.Labels = utils.MergeMaps(
		deploymentLabels,
		deployment.Spec.Template.Labels,
		l.AIDeployment.Spec.Deployment.Labels,
	)

	deployment.Spec.Template.Annotations = utils.MergeMaps(
		deployment.Spec.Template.Annotations,
		l.AIDeployment.Spec.Deployment.Annotations,
	)

	deployment.ObjectMeta = objMeta
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: deploymentLabels}

	return &deployment, nil
}
