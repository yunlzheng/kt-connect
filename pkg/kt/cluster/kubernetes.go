package cluster

import (
	"fmt"

	clusterWatcher "github.com/alibaba/kt-connect/pkg/apiserver/cluster"
	"github.com/alibaba/kt-connect/pkg/kt/util"
	"github.com/alibaba/kt-connect/pkg/kt/vars"
	"github.com/rs/zerolog/log"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"k8s.io/client-go/kubernetes"
)

// GetNamespace get namespace info
func (k *Kubernetes) GetNamespace(name string) (*v1.Namespace, error) {
	return k.Clientset.CoreV1().Namespaces().Get(name, metav1.GetOptions{})
}

// RemoveService remove sevice
func (k *Kubernetes) RemoveService(name, namespace string) (err error) {
	client := k.Clientset.CoreV1().Services(namespace)
	return client.Delete(name, &metav1.DeleteOptions{})
}

// RemoveDeployment remove deployment instances
func (k *Kubernetes) RemoveDeployment(name, namespace string) (err error) {
	deploymentsClient := k.Clientset.AppsV1().Deployments(namespace)
	deletePolicy := metav1.DeletePropagationBackground
	return deploymentsClient.Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
}

// RemoveConfigMap remove ConfigMap instance
func (k *Kubernetes) RemoveConfigMap(name, namespace string) (err error) {
	cli := k.Clientset.CoreV1().ConfigMaps(namespace)
	deletePolicy := metav1.DeletePropagationBackground
	return cli.Delete(name, &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
}

// ScaleTo scale deployment to
func (k *Kubernetes) ScaleTo(deployment, namespace string, replicas *int32) (err error) {
	obj, err := k.Deployment(deployment, namespace)
	if err != nil {
		return
	}
	return k.Scale(obj, replicas)
}

// Scale scale deployment to
func (k *Kubernetes) Scale(deployment *appv1.Deployment, replicas *int32) (err error) {
	log.Info().Msgf("scale deployment %s to %d\n", deployment.GetObjectMeta().GetName(), *replicas)
	client := k.Clientset.AppsV1().Deployments(deployment.GetObjectMeta().GetNamespace())
	deployment.Spec.Replicas = replicas

	d, err := client.Update(deployment)
	if err != nil {
		log.Error().Msgf("%s Fails scale deployment %s to %d\n", err.Error(), deployment.GetObjectMeta().GetName(), *replicas)
		return
	}
	log.Info().Msgf(" * %s (%d replicas) success", d.Name, *d.Spec.Replicas)
	return
}

// Deployment get deployment
func (k *Kubernetes) Deployment(name, namespace string) (*appv1.Deployment, error) {
	return k.Clientset.AppsV1().Deployments(namespace).Get(name, metav1.GetOptions{})
}

// CreateShadow create shadow
func (k *Kubernetes) CreateShadow(name, namespace, image string, labels map[string]string, debug bool) (podIP, podName, sshcm string, credential *util.SSHCredential, err error) {
	component, version := labels["kt-component"], labels["version"]
	sshcm = fmt.Sprintf("kt-%s-public-key-%s", component, version)

	generator, err := util.Generate(util.PrivateKeyPath(component, version))
	if err != nil {
		return
	}

	clientSet := k.Clientset

	labels["kt"] = sshcm
	cli := clientSet.CoreV1().ConfigMaps(namespace)
	configMap, err := cli.Create(&v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sshcm,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			vars.SSHAuthKey: string(generator.PublicKey),
		},
	})
	if err != nil {
		return
	}

	log.Info().Msgf("successful create ssh config map %v", configMap.ObjectMeta.Name)

	localIPAddress := util.GetOutboundIP()
	log.Info().Msgf("Client address %s", localIPAddress)
	labels["remoteAddress"] = localIPAddress

	labels["kt"] = name
	client := clientSet.AppsV1().Deployments(namespace)
	deployment := deployment(namespace, name, labels, image, sshcm, debug)
	log.Info().Msg("shadow template is prepare ready.")
	result, err := client.Create(deployment)
	if err != nil {
		return
	}
	log.Info().Msgf("deploy shadow deployment %s in namespace %s\n", result.GetObjectMeta().GetName(), namespace)

	pod, err := waitPodReadyUsingInformer(namespace, name, clientSet)
	if err != nil {
		return
	}
	podIP = pod.Status.PodIP
	podName = pod.GetObjectMeta().GetName()
	credential = util.NewDefaultSSHCredential()
	credential.PrivateKeyPath = generator.PrivateKeyPath
	return
}

// CreateService create kubernetes service
func (k *Kubernetes) CreateService(name, namespace string, port int, labels map[string]string) (*v1.Service, error) {
	cli := k.Clientset.CoreV1().Services(namespace)
	svc := service(name, namespace, labels, port)
	return cli.Create(svc)
}

// ClusterCrids get cluster cirds
func (k *Kubernetes) ClusterCrids(podCIDR string) (cidrs []string, err error) {
	serviceList, err := k.Clientset.CoreV1().Services("").List(metav1.ListOptions{})
	if err != nil {
		return
	}

	cidrs, err = getPodCirds(k.Clientset, podCIDR)
	if err != nil {
		return
	}

	services := serviceList.Items
	serviceCird, err := getServiceCird(services)
	if err != nil {
		return
	}
	cidrs = append(cidrs, serviceCird...)
	return
}

// ServiceHosts get service dns map
func (k *Kubernetes) ServiceHosts(namespace string) (hosts map[string]string) {
	services, err := k.Clientset.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		return
	}
	hosts = map[string]string{}
	for _, service := range services.Items {
		hosts[service.ObjectMeta.Name] = service.Spec.ClusterIP
	}
	return
}

func waitPodReadyUsingInformer(namespace, name string, clientset kubernetes.Interface) (pod v1.Pod, err error) {
	stopSignal := make(chan struct{})
	defer close(stopSignal)
	podListener, err := clusterWatcher.PodListener(clientset, stopSignal)
	if err != nil {
		return
	}
	pod = v1.Pod{}
	podLabels := labels.NewSelector()
	log.Info().Msgf("pod label: kt=%s", name)
	labelKeys := []string{
		"kt",
	}
	requirement, err := labels.NewRequirement(labelKeys[0], selection.Equals, []string{name})
	if err != nil {
		return
	}
	podLabels.Add(*requirement)

	pods, err := podListener.Pods(namespace).List(podLabels)
	if err != nil {
		return pod, err
	}
wait_loop:
	for {
		hasRunningPod := len(pods) > 0
		var podName string
		if hasRunningPod {
			// podLister do not support FieldSelector
			// https://github.com/kubernetes/client-go/issues/604
			p := getTargetPod(name, labelKeys, pods)
			if p != nil {
				if p.Status.Phase == "Running" {
					pod = *p
					log.Info().Msgf("Shadow pod: %s is ready.", pod.Name)
					break wait_loop
				}
				podName = p.Name
			}
		}
		wait(podName)
		pods, err = podListener.Pods(namespace).List(podLabels)
		if err != nil {
			return pod, err
		}
	}
	return pod, nil
}
