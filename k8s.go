package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	routeApi "github.com/openshift/api/route/v1"
	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (s *ServerSettings) exposeService(appLabel string) (string, error) {
	// Find created
	svcList, err := s.k8sClient.CoreV1().Services(s.namespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil || svcList.Items == nil || len(svcList.Items) == 0 {
		return "", fmt.Errorf("Failed to list services: %v", err)
	}
	svc := svcList.Items[0]
	serviceName := svc.Name
	servicePort := svc.Spec.Ports[0].TargetPort

	promRoute := &routeApi.Route{}
	objectMeta := metav1.ObjectMeta{}
	objectMeta.Name = appLabel
	objectMeta.Namespace = s.namespace
	routeLabels := make(map[string]string)
	routeLabels["app"] = appLabel
	objectMeta.Labels = routeLabels
	promRoute.ObjectMeta = objectMeta

	routeSpec := routeApi.RouteSpec{}

	routeTarget := routeApi.RouteTargetReference{}
	routeTarget.Kind = "Service"
	routeTarget.Name = serviceName
	routeSpec.To = routeTarget

	routePort := &routeApi.RoutePort{}
	routePort.TargetPort = servicePort
	routeSpec.Port = routePort

	tlsConfig := &routeApi.TLSConfig{}
	tlsConfig.Termination = routeApi.TLSTerminationEdge
	tlsConfig.InsecureEdgeTerminationPolicy = routeApi.InsecureEdgeTerminationPolicyRedirect
	routeSpec.TLS = tlsConfig

	promRoute.Spec = routeSpec
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
			return false, fmt.Errorf("Failed to list deployments: %v", err)
		}
		if len(deps.Items) != 1 {
			return true, fmt.Errorf("No running deployments found")
		}
		dep := deps.Items[0]
		if dep.Status.Replicas == 0 {
			return false, fmt.Errorf("Zero pod replicas")
		}
		return dep.Status.ReadyReplicas == dep.Status.Replicas, nil
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
