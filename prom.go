package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os/exec"
	"strings"
	"time"
)

const namespacePrefix = "prom-"
const charset = "abcdefghijklmnopqrstuvwxyz"
const randLength = 8
const promTemplates = "prom-templates"

// Generate random namespace name
func (e *Env) generateNamespace() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return fmt.Sprintf("%s%s", namespacePrefix, string(b))
}

// Create a temp kustomize file and apply manifests
func (e *Env) createPrometheus(namespace string, metricsTar string) error {
	// Create namespace
	cmd := exec.Command("oc", "new-project", namespace)
	output, err := cmd.CombinedOutput()
	log.Printf(string(output))

	if err != nil {
		log.Printf(err.Error())
		return err
	}

	// Make temp dir for assets
	dir, err := ioutil.TempDir("", namespace)
	// defer os.RemoveAll(dir)
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	promTemplatesDir := fmt.Sprintf("%s/%s", dir, promTemplates)

	err = RestoreAssets(dir, promTemplates)
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	// Replace path to fetch metrics
	deploymentPath := fmt.Sprintf("%s/%s", promTemplatesDir, "deployment.yaml")
	read, err := ioutil.ReadFile(deploymentPath)
	if err != nil {
		return err
	}
	newContents := strings.Replace(string(read), "PROMTAR_VALUE", metricsTar, -1)
	err = ioutil.WriteFile(deploymentPath, []byte(newContents), 0)
	if err != nil {
		return err
	}
	log.Println("deployment was rewritten")

	// Apply kustomization
	cmd = exec.Command("oc", "-n", namespace, "apply", "-f", promTemplatesDir)
	output, err = cmd.CombinedOutput()
	log.Printf(string(output))

	if err != nil {
		log.Printf(err.Error())
		return err
	}

	return nil
}

func runOcCommand(namespace string, args []string) (string, error) {
	namespacedArgs := append([]string{"-n", namespace}, args...)
	output, err := exec.Command("oc", namespacedArgs...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) (string, error) {
	deploymentRolledOut := []string{"wait", "--for=condition=Available", "deployment/prom"}
	_, err := runOcCommand(namespace, deploymentRolledOut)
	if err != nil {
		return "", err
	}

	routeCmd := []string{"get", "-o", "name", "route/prom"}
	route, err := runOcCommand(namespace, routeCmd)
	if err != nil {
		return "", err
	}

	return route, nil
}
