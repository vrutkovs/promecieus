package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ServerSettings stores info about the server
type ServerSettings struct {
	statusWebSocket *websocket.Conn
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	prowPrefix    = "https://prow.svc.ci.openshift.org/view/"
	gcsPrefix     = "https://gcsweb-ci.svc.ci.openshift.org/"
	promTarPath   = "artifacts/e2e-aws/metrics/prometheus.tar"
)

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

func exposeService(appLabel string) (string, error) {
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

func getRouteHost(appLabel string) (string, error) {
	routeCmd := []string{"get", "route", appLabel, "-o", "jsonpath=http://{.spec.host}"}
	route, err := exec.Command("oc", routeCmd...).Output()
	if err != nil {
		return "", err
	}
	return string(route), nil
}

func waitForPodToStart(appLabel string) error {
	podRolledOut := []string{"wait", "pod", "--for=condition=Ready", "-l", fmt.Sprintf("app=%s", appLabel)}
	output, err := exec.Command("oc", podRolledOut...).CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		return err
	}
	return nil
}

func loadTemplate() (*template.Template, error) {
	t := template.New("")
	for _, name := range AssetNames() {
		file, err := AssetInfo(name)
		if err != nil || file.IsDir() || !strings.HasPrefix(name, "html/") {
			continue
		}
		contents, err := Asset(name)
		if err != nil {
			return nil, err
		}
		t, err = t.New(name).Parse(string(contents))
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}
