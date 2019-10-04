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

func runOcCommand(namespace string, args []string) (string, error) {
	namespacedArgs := append([]string{"-n", namespace}, args...)
	output, err := exec.Command("oc", namespacedArgs...).Output()
	log.Printf(string(output))
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// Upload metrics tar to prometheus
func (e *Env) uploadMetrics(namespace string, metricsTarUrl string) error {
	deploymentRolledOut := []string{"wait", "--for=condition=Available", "deployment/prom"}
	_, err := runOcCommand(namespace, deploymentRolledOut)
	if err != nil {
		return err
	}

	promPodNameCmd := []string{"get", "pods", "-o", "name"}
	promPodName, err := runOcCommand(namespace, promPodNameCmd)
	if err != nil {
		return err
	}

	execPod := []string{"exec", "pod", string(promPodName)}

	wgetInPod := append(execPod, "wget", metricsTarUrl)
	_, err = runOcCommand(namespace, wgetInPod)
	if err != nil {
		return err
	}

	unpackInPod := append(execPod, "tar", "-xvz", promTarName)
	_, err = runOcCommand(namespace, unpackInPod)
	if err != nil {
		return err
	}

	restartProm := []string{"exec", "kill", "-HUP", "1"}
	_, err = runOcCommand(namespace, restartProm)
	if err != nil {
		return err
	}
	return nil
}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) {}
