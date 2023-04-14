package promecieus

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	routeApi "github.com/openshift/api/route/v1"
	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	deploymentRolloutTime = time.Minute
	deploymentLifetime    = 4 * time.Hour
	// This is a custom prometheus image to ignore reading corrupted WAL records.
	// Build instructions are here: https://quay.io/repository/tjungblu/patched-prometheus?tab=info
	// Code in this branch: https://github.com/tjungblu/prometheus/tree/ignorecheck
	// For a proper solution, please see: https://github.com/openshift/release/pull/33210
	// This was based on Prometheus v2.39.1.
	prometheusImage = "quay.io/tjungblu/patched-prometheus:ignorecheck-e1feeff"
	ciFetcherImage  = "registry.access.redhat.com/ubi8/ubi:8.6"
	promAppLabel    = "%s-prom"
)

var (
	ErrorContainerLog = errors.New("failed to start prometheus")
)

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

// TryLogin returns k8s clientset and route client
func TryLogin(kubeconfigPath string) (*k8s.Clientset, *routeClient.RouteV1Client, error) {
	config, err := buildConfig(kubeconfigPath)
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

func (s *ServerSettings) launchPromApp(ctx context.Context, appLabel string, metricsTar string) (string, error) {
	replicas := int32(1)
	sharePIDNamespace := true

	// Declare and create new deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(promAppLabel, appLabel),
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
							Image: ciFetcherImage,
							Command: []string{
								"/bin/bash",
								"-c",
								"set -uxo pipefail && umask 0000 && curl -sL ${PROMTAR} | tar xvz -m --no-overwrite-dir",
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
							Image: prometheusImage,
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
								ProbeHandler: corev1.ProbeHandler{
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
	_, err := s.K8sClient.AppsV1().Deployments(s.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create new deployment: %s", err.Error())
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
	_, err = s.K8sClient.CoreV1().Services(s.Namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create new service: %s", err.Error())
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
	route, err := s.RouteClient.Routes(s.Namespace).Create(ctx, promRoute, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create route: %v", err)
	}

	return fmt.Sprintf("https://%s", route.Spec.Host), nil
}

func (s *ServerSettings) waitForEndpointReady(ctx context.Context, promRoute string) error {
	timer := time.NewTicker(time.Second)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			response, err := client.Get(promRoute + "/-/ready")
			if err != nil {
				log.Printf("getting [%v] resulted in err [%v], retrying...", promRoute, err)
				continue
			}
			if response.StatusCode != 200 {
				log.Printf("getting [%v] returned non-OK status code [%v], retrying...", promRoute, response.StatusCode)
			} else {
				log.Printf("prometheus is ready: %v", promRoute)
				return nil
			}
		}
	}
}

func (s *ServerSettings) waitForDeploymentReady(ctx context.Context, appLabel string) error {
	deploymentName := fmt.Sprintf(promAppLabel, appLabel)
	log.Printf("watching deployment %s", deploymentName)
	watcher, err := s.K8sClient.AppsV1().Deployments(s.Namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch deployment: %v", err)
	}
	defer watcher.Stop()

	timer := time.NewTimer(deploymentRolloutTime)
	for {
		select {
		case event := <-watcher.ResultChan():
			deployment, ok := event.Object.(*appsv1.Deployment)
			if !ok {
				log.Printf("invalid object watched: %#v", deployment)
				continue
			}
			log.Printf("deployment status: %#v", deployment.Status)
			if deployment.Status.AvailableReplicas == 1 {
				return nil
			}
		case <-timer.C:
			log.Printf("timed out waiting for deployment %s to rollout", deploymentName)
			// Find the pod created by this deployment
			listOpts := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			}
			podList, err := s.K8sClient.CoreV1().Pods(s.Namespace).List(ctx, listOpts)
			if err != nil {
				return fmt.Errorf("error finding pods created by deployment %s: %v, please report this to #forum-crt", deploymentName, err)
			}
			if podList.Items == nil || len(podList.Items) < 1 {
				return fmt.Errorf("failed to list pods created by deployment %s: %#v, please report this to #forum-crt", deploymentName, podList)
			}
			pod := podList.Items[0] // We only create one pod

			// Find the failing container in initContainers or containers
			failingContainerName := ""
			for _, initContainerStatus := range pod.Status.InitContainerStatuses {
				if !initContainerStatus.Ready {
					failingContainerName = initContainerStatus.Name
					break
				}
			}
			if failingContainerName == "" {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if !containerStatus.Ready {
						failingContainerName = containerStatus.Name
						break
					}
				}
			}
			if failingContainerName == "" {
				return fmt.Errorf("failed to find failing container in pod %s created by deployment %s: %v, please report this to #forum-crt", pod.Name, deploymentName, err)
			}

			// Fetch container logs
			podLogOptions := corev1.PodLogOptions{
				Container: failingContainerName,
				Follow:    false,
			}
			req := s.K8sClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOptions)
			podLogs, err := req.Stream(ctx)
			if err != nil {
				return fmt.Errorf("error opening stream to fetch failing logs in pod %s created by deployment %s: %v, please report this to #forum-crt", pod.Name, deploymentName, err)
			}
			defer podLogs.Close()

			buf := new(bytes.Buffer)
			_, err = io.Copy(buf, podLogs)
			if err != nil {
				return fmt.Errorf("error copying logs in pod %s created by deployment %s: %v, please report this to #forum-crt", pod.Name, deploymentName, err)
			}
			return fmt.Errorf("%w:\n%s", ErrorContainerLog, buf.String())
		}
	}
}

func (s *ServerSettings) deletePods(ctx context.Context, appLabel string) (string, error) {
	actionLog := []string{}

	// Delete service
	listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", appLabel)}
	svcList, err := s.K8sClient.CoreV1().Services(s.Namespace).List(ctx, listOpts)
	if err != nil || svcList.Items == nil {
		return "", fmt.Errorf("failed to find services: %v", err)
	}
	for _, svc := range svcList.Items {
		err := s.K8sClient.CoreV1().Services(s.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("error removing service %s: %v", svc.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed service %s", svc.Name))
	}

	// Delete deployment
	depList, err := s.K8sClient.AppsV1().Deployments(s.Namespace).List(ctx, listOpts)
	if err != nil || depList.Items == nil {
		return "", fmt.Errorf("failed to find deployments: %v", err)
	}
	for _, dep := range depList.Items {
		err := s.K8sClient.AppsV1().Deployments(s.Namespace).Delete(ctx, dep.Name, metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("error removing deployment %s: %v", dep.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed deployment %s", dep.Name))
	}

	// Delete configmap
	cmList, err := s.K8sClient.CoreV1().ConfigMaps(s.Namespace).List(ctx, listOpts)
	if err != nil || cmList.Items == nil {
		return "", fmt.Errorf("failed to find config maps: %v", err)
	}
	for _, cm := range cmList.Items {
		err := s.K8sClient.CoreV1().ConfigMaps(s.Namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("error removing config map %s: %v", cm.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed config map %s", cm.Name))
	}

	// Delete route
	routeList, err := s.RouteClient.Routes(s.Namespace).List(ctx, listOpts)
	if err != nil || routeList.Items == nil {
		return "", fmt.Errorf("failed to find routes: %v", err)
	}
	for _, route := range routeList.Items {
		err := s.RouteClient.Routes(s.Namespace).Delete(ctx, route.Name, metav1.DeleteOptions{})
		if err != nil {
			return strings.Join(actionLog, "\n"),
				fmt.Errorf("error removing route %s: %v", route.Name, err)
		}
		actionLog = append(actionLog, fmt.Sprintf("Removed route %s", route.Name))
	}

	return strings.Join(actionLog, "\n"), nil
}

// CleanupOldDeployements periodically removes old deployments
func (s *ServerSettings) CleanupOldDeployements(ctx context.Context) {
	log.Println("Cleaning up old deployments")
	// List all deployments, find those which are older than n hours and call 'deletePods'
	depsList, err := s.K8sClient.AppsV1().Deployments(s.Namespace).List(ctx, metav1.ListOptions{})
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
			go s.deletePods(ctx, appLabel)
		} else {
			log.Println("Deployment will live see another dawn")
		}
	}
}

// GetResourceQuota updates current resource quota setting
func (s *ServerSettings) GetResourceQuota(ctx context.Context) error {
	rquota, err := s.K8sClient.CoreV1().ResourceQuotas(s.Namespace).Get(ctx, s.RQuotaName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get ResourceQuota: %v", err)
	}
	s.RQStatus = &RQuotaStatus{
		Used: rquota.Status.Used.Pods().Value(),
		Hard: rquota.Status.Hard.Pods().Value(),
	}
	s.sendResourceQuotaUpdate()
	return nil
}

// WatchResourceQuota passes RQ updates from k8s to UI
func (s *ServerSettings) WatchResourceQuota(ctx context.Context) {
	for {
		// TODO: Make sure we watch correct resourceQuota
		watcher, err := s.K8sClient.CoreV1().ResourceQuotas(s.Namespace).Watch(ctx, metav1.ListOptions{})
		if err != nil {
			log.Printf("Failed to setup ResourceQuota watcher: %v", err)
			continue
		}
		ch := watcher.ResultChan()
		for event := range ch {
			rq, ok := event.Object.(*corev1.ResourceQuota)
			if !ok || rq.Name != s.RQuotaName {
				log.Printf("Skipping rq update: %v, %s", ok, rq.Name)
				continue
			}
			s.RQStatus = &RQuotaStatus{
				Used: rq.Status.Used.Pods().Value(),
				Hard: rq.Status.Hard.Pods().Value(),
			}
			log.Printf("ResourceQuota update: %v", s.RQStatus)
			s.sendResourceQuotaUpdate()
		}
	}
}
