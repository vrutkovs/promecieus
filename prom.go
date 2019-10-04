package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"
)

const namespacePrefix = "prom-"
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const randLength = 8

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
	exec.Command("oc", "new-project", namespace)

	// Make temp dir for assets
	dir := os.TempDir()
	defer os.RemoveAll(dir)

	RestoreAssets(dir, "prom-templates")
	// Replace namespace in kustomization.yaml
	newNamespaceSetting := fmt.Sprintf("namespace: %s", namespace)

	kustomizationPath := fmt.Sprintf("%s/%s", dir, "kustomization.yaml")
	read, err := ioutil.ReadFile(kustomizationPath)
	if err != nil {
		return err
	}
	newContents := strings.Replace(string(read), "namespace: prom-test", newNamespaceSetting, -1)
	err = ioutil.WriteFile(kustomizationPath, []byte(newContents), 0)
	if err != nil {
		return err
	}

	// Apply kustomization
	exec.Command("oc", "apply", "-k", dir)

	return nil
}

// Upload metrics tar to prometheus
func (e *Env) uploadMetrics(namespace string, metricsTarUrl string) {}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) {}
