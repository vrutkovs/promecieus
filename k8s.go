package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	routeApi "github.com/openshift/api/route/v1"
	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	deploymentRolloutTime = 5 * time.Minute
	deploymentLifetime    = 8 * time.Hour
)

func inClusterLogin() (*k8s.Clientset, *routeClient.RouteV1Client, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, err
	}
	// Seed random
	rand.Seed(time.Now().Unix())

	// creates the clientset
	k8sClient, err := k8s.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}

	// create route client
	routeClient, err := routeClient.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}

	return k8sClient, routeClient, err

}

func (s *ServerSettings) launchPromApp(appLabel string, metricsTar string) (string, error) {
	replicas := int32(1)
	sharePIDNamespace := true

	// Declare and create new deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-prom", appLabel),
			Labels: map[string]string{
				"app": appLabel,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": appLabel,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": appLabel,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "ci-fetcher",
							Image: "registry.fedoraproject.org/fedora:30",
							Command: []string{
								"/bin/bash",
								"-c",
								"set -uxo pipefail && umask 0000 && curl -sL ${PROMTAR} | tar xvz -m",
							},
							WorkingDir: "/prometheus/",
							Env: []corev1.EnvVar{
								{
									Name:  "PROMTAR",
									Value: metricsTar,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "prometheus",
							Image: "prom/prometheus:v2.12.0",
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 9090,
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(9090),
										Scheme: "HTTP",
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("500Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
						},
					},
					ShareProcessNamespace: &sharePIDNamespace,
					Volumes: []corev1.Volume{
						{
							Name: "prometheus-storage-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
	_, err := s.k8sClient.AppsV1().Deployments(s.namespace).Create(deployment)
	if err != nil {
		return "", fmt.Errorf("Failed to create new deployment: %s", err.Error())
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: appLabel,
			Labels: map[string]string{
				"app": appLabel,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     9090,
					Protocol: corev1.ProtocolTCP,
					Name:     "webui",
				},
			},
			Selector: map[string]string{
				"app": appLabel,
			},
		},
	}
	_, err = s.k8sClient.CoreV1().Services(s.namespace).Create(service)
	if err != nil {
		return "", fmt.Errorf("Failed to create new service: %s", err.Error())
	}

	promRoute := &routeApi.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name: appLabel,
			Labels: map[string]string{
				"app": appLabel,
			},
		},
		Spec: routeApi.RouteSpec{
			To: routeApi.RouteTargetReference{
				Kind: "Service",
				Name: appLabel,
			},
			Port: &routeApi.RoutePort{
				TargetPort: intstr.FromInt(9090),
			},
			TLS: &routeApi.TLSConfig{
				Termination:                   routeApi.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routeApi.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}
	route, err := s.routeClient.Routes(s.namespace).Create(promRoute)
	if err != nil {
		return "", fmt.Errorf("Failed to create route: %v", err)
	}

	return fmt.Sprintf("https://%s", route.Spec.Host), nil
}

func (s *ServerSettings) waitForDeploymentReady(appLabel string) error {
	return wait.PollImmediate(time.Second, deploymentRolloutTime, func() (bool, error) {
		listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", appLabel)}
		deps, err := s.k8sClient.AppsV1().Deployments(s.namespace).List(listOpts)
		if err != nil {
			return true, fmt.Errorf("Failed to list deployments: %v", err)
		}
		if len(deps.Items) != 1 {
			log.Println("No running deployments found")
			return false, nil
		}
		dep := deps.Items[0]
		log.Printf("AvailableReplicas: %d", dep.Status.AvailableReplicas)
		return dep.Status.AvailableReplicas == 1, nil
	})
}

func (s *ServerSettings) deletePods(appLabel string) (string, error) {
	actionLog := []string{}

	// Delete service
	listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", appLabel)}
	svcList, err := s.k8sClient.CoreV1().Services(s.namespace).List(listOpts)
	if err != nil || svcList.Items == nil {
		return "", fmt.Errorf("Failed to find services: %v", err)
	}
	for _, svc := range svcList.Items {
		err := s.k8sClient.CoreV1().Services(s.namespace).Delete(svc.Name, &metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("Error removing service %s: %v", svc.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed service %s", svc.Name))
	}

	// Delete deployment
	depList, err := s.k8sClient.AppsV1().Deployments(s.namespace).List(listOpts)
	if err != nil || depList.Items == nil {
		return "", fmt.Errorf("Failed to find deployments: %v", err)
	}
	for _, dep := range depList.Items {
		err := s.k8sClient.AppsV1().Deployments(s.namespace).Delete(dep.Name, &metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("Error removing deployment %s: %v", dep.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed deployment %s", dep.Name))
	}

	// Delete configmap
	cmList, err := s.k8sClient.CoreV1().ConfigMaps(s.namespace).List(listOpts)
	if err != nil || cmList.Items == nil {
		return "", fmt.Errorf("Failed to find config maps: %v", err)
	}
	for _, cm := range cmList.Items {
		err := s.k8sClient.CoreV1().ConfigMaps(s.namespace).Delete(cm.Name, &metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("Error removing config map %s: %v", cm.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed config map %s", cm.Name))
	}

	// Delete route
	routeList, err := s.routeClient.Routes(s.namespace).List(listOpts)
	if err != nil || routeList.Items == nil {
		return "", fmt.Errorf("Failed to find routes: %v", err)
	}
	for _, route := range routeList.Items {
		err := s.routeClient.Routes(s.namespace).Delete(route.Name, &metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("Error removing route %s: %v", route.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed route %s", route.Name))
	}

	return strings.Join(actionLog, "\n"), nil
}

func (s *ServerSettings) cleanupOldDeployements() {
	log.Println("Cleaning up old deployments")
	// List all deployments, find those which are older than n hours and call 'deletePods'
	depsList, err := s.k8sClient.AppsV1().Deployments(s.namespace).List(metav1.ListOptions{})
	if err != nil || depsList.Items == nil {
		return
	}
	now := time.Now()
	for _, dep := range depsList.Items {
		log.Printf("Found %s", dep.Name)
		// Get dep label and create time
		appLabel, ok := dep.Labels["app"]
		if !ok {
			log.Println("Deployment has no appLabel, skipping")
			// Deployment has no app label
			continue
		}
		createdAt := dep.GetCreationTimestamp()
		if now.After(createdAt.Add(deploymentLifetime)) {
			log.Println("Deployment will be garbage collected")
			go s.deletePods(appLabel)
		} else {
			log.Println("Deployment will live see another dawn")
		}
	}
}

func (s *ServerSettings) getResourceQuota() error {
	rquota, err := s.k8sClient.CoreV1().ResourceQuotas(s.namespace).Get(s.rquotaName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to setup ResourceQuota watcher: %v", err)
	}
	s.rqStatus = RQuotaStatus{
		Used: rquota.Status.Used.Pods().Value(),
		Hard: rquota.Status.Hard.Pods().Value()}
	return nil
}

func (s *ServerSettings) watchResourceQuota() error {
	// TODO: Make sure we watch correct resourceQuota
	watcher, err := s.k8sClient.CoreV1().ResourceQuotas(s.namespace).Watch(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Failed to setup ResourceQuota watcher: %v", err)
	}
	ch := watcher.ResultChan()
	for event := range ch {
		rq, ok := event.Object.(*corev1.ResourceQuota)
		if !ok || rq.Name != s.rquotaName {
			log.Printf("Skipping rq update: %v, %s", ok, rq.Name)
			continue
		}
		s.rqStatus = RQuotaStatus{
			Used: rq.Status.Used.Pods().Value(),
			Hard: rq.Status.Hard.Pods().Value(),
		}
		log.Printf("ResourceQuota update: %v", s.rqStatus)
		s.sendResourceQuotaUpdate()
	}
	return nil
}
