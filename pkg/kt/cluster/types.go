package cluster

import (
	"github.com/alibaba/kt-connect/pkg/kt/util"
	appV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Create kubernetes instance
func Create(kubeConfig string) (kubernetes KubernetesInterface, err error) {
	clientSet, err := getKubernetesClient(kubeConfig)
	if err != nil {
		return
	}
	return &Kubernetes{
		Clientset: clientSet,
	}, nil
}

// KubernetesInterface kubernetes interface
type KubernetesInterface interface {
	GetNamespace(name string) (*v1.Namespace, error)
	RemoveDeployment(name, namespace string) (err error)
	RemoveConfigMap(name, namespace string) (err error)
	RemoveService(name, namespace string) (err error)
	Deployment(name, namespace string) (deployment *appV1.Deployment, err error)
	Scale(deployment *appV1.Deployment, replicas *int32) (err error)
	ScaleTo(deployment, namespace string, replicas *int32) (err error)
	ServiceHosts(namespace string) (hosts map[string]string)
	ClusterCrids(podCIDR string) (cidrs []string, err error)
	CreateShadow(name, namespace, image string, labels map[string]string, debug bool) (podIP, podName, sshcm string, credential *util.SSHCredential, err error)
	CreateService(name, namespace string, port int, labels map[string]string) (*v1.Service, error)
}

// Kubernetes implements KubernetesInterface
type Kubernetes struct {
	KubeConfig string
	Clientset  kubernetes.Interface
}
