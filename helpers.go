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

	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ServerSettings stores info about the server
type ServerSettings struct {
	statusWebSocket *websocket.Conn
	k8sClient       *k8s.Clientset
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	prowPrefix    = "https://prow.svc.ci.openshift.org/view/"
	gcsPrefix     = "https://gcsweb-ci.svc.ci.openshift.org/"
	promTarPath   = "artifacts/e2e-aws/metrics/prometheus.tar"
)

func inClusterLogin() (*k8s.Clientset, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// Seed random
	rand.Seed(time.Now().Unix())

	// creates the clientset
	return k8s.NewForConfig(config)
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

func (s *ServerSettings) applyKustomize(appLabel string, metricsTar string) (string, error) {
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
	svcCmd := []string{"get", "-o", "name", "service", "-l", fmt.Sprintf("app=%s", appLabel)}
	service, err := exec.Command("oc", svcCmd...).Output()
	if err != nil {
		return "", err
	}
	serviceName := strings.Split(string(service), "\n")[0]

	exposeSvc := []string{"expose", serviceName, "-l", fmt.Sprintf("app=%s", appLabel), "--name", appLabel}
	output, err := exec.Command("oc", exposeSvc...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (s *ServerSettings) getRouteHost(appLabel string) (string, error) {
	routeCmd := []string{"get", "route", appLabel, "-o", "jsonpath=http://{.spec.host}"}
	route, err := exec.Command("oc", routeCmd...).Output()
	if err != nil {
		return "", err
	}
	return string(route), nil
}

func (s *ServerSettings) waitForPodToStart(appLabel string) (string, error) {
	podRolledOut := []string{
		"wait", "pod", "--for=condition=Ready", "--timeout=5m", "-l", fmt.Sprintf("app=%s", appLabel)}
	output, err := exec.Command("oc", podRolledOut...).CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func (s *ServerSettings) deletePods(appLabel string) (string, error) {
	deleteAll := []string{
		"delete", "all", "-l", fmt.Sprintf("app=%s", appLabel)}
	deleteAllOutput, err := exec.Command("oc", deleteAll...).CombinedOutput()
	if err != nil {
		return string(deleteAllOutput), err
	}

	deleteConfigMaps := []string{
		"delete", "cm", "-l", fmt.Sprintf("app=%s", appLabel)}
	deleteConfigMapsOutput, err := exec.Command("oc", deleteConfigMaps...).CombinedOutput()
	if err != nil {
		return string(deleteConfigMapsOutput), err
	}
	return fmt.Sprintf("%s\n%s", string(deleteAllOutput), string(deleteConfigMapsOutput)), nil
}
