package resources

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkv1 "k8s.io/api/networking/v1"
)

func DesiredIngress(owner metav1.Object, name, namespace string, hostname []string, svcName string, port int, labels, annotations map[string]string, tls bool) *networkv1.Ingress {
	t := networkv1.PathType("Prefix")
	rules := []networkv1.IngressRule{}
	for _, h := range hostname {
		rules = append(rules, networkv1.IngressRule{
			Host: h,
			IngressRuleValue: networkv1.IngressRuleValue{
				HTTP: &networkv1.HTTPIngressRuleValue{
					Paths: []networkv1.HTTPIngressPath{{
						PathType: &t,
						Path:     "/",
						Backend: networkv1.IngressBackend{
							Service: &networkv1.IngressServiceBackend{
								Name: svcName,
								Port: networkv1.ServiceBackendPort{Number: int32(port)},
							},
						},
					}},
				},
			},
		})
	}

	spec := networkv1.IngressSpec{
		Rules: rules,
	}
	if labels == nil {
		labels = map[string]string{}
	}
	if annotations == nil {
		annotations = map[string]string{}
	}

	if tls {
		tlsEntry := []networkv1.IngressTLS{
			{
				Hosts:      hostname,
				SecretName: fmt.Sprintf("%s-tls", svcName),
			}}
		spec.TLS = tlsEntry
	}

	return &networkv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: GenOwner(owner),
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			Annotations:     annotations,
		},
		Spec: spec,
	}
}
