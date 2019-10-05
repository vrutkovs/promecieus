package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	routeApi "github.com/openshift/api/route/v1"
	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ServerSettings stores info about the server
type ServerSettings struct {
	statusWebSocket *websocket.Conn
	k8sClient       *k8s.Clientset
	routeClient     *routeClient.RouteV1Client
	namespace       string
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	prowPrefix    = "https://prow.svc.ci.openshift.org/view/"
	gcsPrefix     = "https://gcsweb-ci.svc.ci.openshift.org/"
	promTarPath   = "artifacts/e2e-aws/metrics/prometheus.tar"
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

func generateAppLabel() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func getMetricsTar(url string) string {
	// Get artifacts link
	artifactsUrl := strings.Replace(url, prowPrefix, gcsPrefix, -1)

	// Get a link to prometheus metadata
	// TODO: aws is hardcoded :/
	metricsTar := fmt.Sprintf("%s/%s", artifactsUrl, promTarPath)
	log.Printf("metricsTar: %s", metricsTar)
	return metricsTar
}

func updateKustomization(tmpDir string, metricsTar string, appLabel string) error {
	// Replace path to fetch metrics and set common labels
	deploymentPath := fmt.Sprintf("%s/%s", tmpDir, "kustomization.yaml")
	read, err := ioutil.ReadFile(deploymentPath)
	if err != nil {
		return err
	}
	newContents := strings.Replace(string(read), "PROMTAR_VALUE", metricsTar, -1)
	newContents = strings.Replace(newContents, "COMMON_LABEL", appLabel, -1)
	err = ioutil.WriteFile(deploymentPath, []byte(newContents), 0)
	if err != nil {
		return err
	}
	return nil
}

func applyKustomize(appLabel string, metricsTar string) (string, error) {
	// Make temp dir for assets
	tmpDir, err := ioutil.TempDir("", appLabel)
	defer os.RemoveAll(tmpDir)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	err = RestoreAssets(tmpDir, promTemplates)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	promTemplatesDir := fmt.Sprintf("%s/%s", tmpDir, promTemplates)
	err = updateKustomization(promTemplatesDir, metricsTar, appLabel)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	// Apply manifests via kustomize
	cmd := exec.Command("oc", "apply", "-k", promTemplatesDir)
	output, err := cmd.CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		log.Printf(err.Error())
		return string(output), err
	}

	return string(output), nil
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

func (s *ServerSettings) waitForPodToStart(appLabel string) error {
	return wait.PollImmediate(time.Second*30, time.Second*5, func() (bool, error) {
		listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", appLabel)}
		pods, err := s.k8sClient.CoreV1().Pods(s.namespace).List(listOpts)
		if err != nil {
			return false, fmt.Errorf("Failed to find pods: %v", err)
		}
		if len(pods.Items) != 1 {
			return false, fmt.Errorf("Wrong number of pods found: %d", len(pods.Items))
		}
		pod := pods.Items[0]
		return pod.Status.Phase == v1.PodRunning, nil
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
