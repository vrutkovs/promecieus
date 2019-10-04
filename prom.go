package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"
)

const charset = "abcdefghijklmnopqrstuvwxyz"
const randLength = 8
const promTemplates = "prom-templates"

// Generate random app label name
func generateAppLabel() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// Create a temp kustomize file and apply manifests
func createPrometheus(appLabel string, metricsTar string) error {
	// Make temp dir for assets
	dir, err := ioutil.TempDir("", appLabel)
	defer os.RemoveAll(dir)
	if err != nil {
		log.Printf(err.Error())
		return err
	}

	err = RestoreAssets(dir, promTemplates)
	if err != nil {
		log.Printf(err.Error())
		return err
	}

	promTemplatesDir := fmt.Sprintf("%s/%s", dir, promTemplates)
	err = updateKustomization(promTemplatesDir, metricsTar, appLabel)
	if err != nil {
		log.Printf(err.Error())
		return err
	}

	// Apply manifests via kustomize
	cmd := exec.Command("oc", "apply", "-k", promTemplatesDir)
	output, err := cmd.CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		log.Printf(err.Error())
		return err
	}

	return nil
}

// Set common label and metrics Tar in the kustomize apps
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

// Get prometheus route URL
func getPromRoute(appLabel string) (string, error) {
	log.Println("Waiting for pods to rollout")
	podRolledOut := []string{"wait", "pod", "--timeout=30m", "--for=condition=Ready", "-l", fmt.Sprintf("app=%s", appLabel)}
	output, err := exec.Command("oc", podRolledOut...).CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}
	log.Println("Pods are ready")

	log.Println("Fetching service name")
	svcCmd := []string{"get", "-o", "name", "service", "-l", fmt.Sprintf("app=%s", appLabel)}
	service, err := exec.Command("oc", svcCmd...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}

	exposeSvc := []string{"expose", string(service), "pod", "-l", fmt.Sprintf("app=%s", appLabel), "--name", appLabel}
	output, err = exec.Command("oc", exposeSvc...).CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}
	log.Println("Route created")

	log.Println("Fetching route host")
	routeCmd := []string{"get", "-o", "jsonpath=https://{.spec.host}", "route", "-l", fmt.Sprintf("app=%s", appLabel)}
	route, err := exec.Command("oc", routeCmd...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}

	return string(route), nil
}
