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

const namespacePrefix = "prom-"
const charset = "abcdefghijklmnopqrstuvwxyz"
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
	cmd := exec.Command("oc", "new-project", namespace)
	output, err := cmd.CombinedOutput()
	log.Printf(string(output))

	if err != nil {
		log.Printf(err.Error())
		return err
	}

	// Make temp dir for assets
	dir, err := ioutil.TempDir(os.TempDir(), namespace)
	defer os.RemoveAll(dir)
	if err != nil {
		log.Printf(err.Error())
		return err
	}
	log.Println(dir)

	err = RestoreAssets(dir, "prom-templates")
	if err != nil {
		log.Printf(err.Error())
		return err
	}
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
	log.Println("kustomize was rewritten")

	// Apply kustomization
	cmd = exec.Command("oc", "apply", "-k", dir)
	output, err = cmd.CombinedOutput()
	log.Printf(string(output))

	if err != nil {
		log.Printf(err.Error())
		return err
	}

	return nil
}

// Upload metrics tar to prometheus
func (e *Env) uploadMetrics(namespace string, metricsTarUrl string) {}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) {}
