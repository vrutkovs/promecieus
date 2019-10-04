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
func (e *Env) createPrometheus(namespace string) error {
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
	log.Println(dir)
	promTemplatesDir := fmt.Sprintf("%s/%s", dir, promTemplates)

	err = RestoreAssets(dir, promTemplates)
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	// Replace namespace in kustomization.yaml
	newNamespaceSetting := fmt.Sprintf("namespace: %s", namespace)

	kustomizationPath := fmt.Sprintf("%s/%s", promTemplatesDir, "kustomization.yaml")
	read, err := ioutil.ReadFile(kustomizationPath)
	if err != nil {
		return err
	}
	newContents := strings.Replace(string(read), "namespace: prom-test", newNamespaceSetting, -1)
	err = ioutil.WriteFile(kustomizationPath, []byte(newContents), 0)
	if err != nil {
		return err
	}
	log.Println("kustomize was rewritten")

	// Apply kustomization
	cmd = exec.Command("oc", "apply", "-k", promTemplatesDir)
	output, err = cmd.CombinedOutput()
	log.Printf(string(output))

	if err != nil {
		log.Printf(err.Error())
		return err
	}

	return nil
}

// Upload metrics tar to prometheus
func (e *Env) uploadMetrics(namespace string, metricsTarUrl string) error {
	promPodNameCmd := []string{"-n", namespace, "get", "pods", "-o", "name"}
	promPodName, err := exec.Command("oc", promPodNameCmd...).Output()
	if err != nil {
		log.Printf(string(promPodName))
		return err
	}
	wgetInPod := []string{"-n", namespace, "exec", "pod", string(promPodName), "wget", metricsTarUrl}
	output, err := exec.Command("oc", wgetInPod...).Output()
	if err != nil {
		log.Printf(string(output))
		return err
	}

	unpackInPod := []string{"-n", namespace, "exec", "pod", string(promPodName), "tar", "-xvz", promTarName}
	output, err = exec.Command("oc", unpackInPod...).Output()
	if err != nil {
		log.Printf(string(output))
		return err
	}

	restartProm := []string{"-n", namespace, "exec", "kill", "-HUP", "1"}
	output, err = exec.Command("oc", restartProm...).Output()
	if err != nil {
		log.Printf(string(output))
		return err
	}
	return nil
}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) {}
