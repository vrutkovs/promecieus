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

const appLabelPrefix = "prom-"
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
	return fmt.Sprintf("%s%s", appLabelPrefix, string(b))
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
	deploymentRolledOut := []string{"wait", "--for=condition=Ready", "pod"}
	output, err := exec.Command("oc", deploymentRolledOut...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}

	routeCmd := []string{"get", "-o", "jsonpath=https://{.spec.host}", "route", "-l", fmt.Sprintf("app=%s", appLabel)}
	route, err := exec.Command("oc", routeCmd...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}
	if err != nil {
		return "", err
	}

	return string(route), nil
}
