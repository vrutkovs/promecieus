package main

import (
	"fmt"
	"math/rand"
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
	return fmt.Sprintf("%s-%s", namespacePrefix, string(b))
}

// Create a temp kustomize file and apply manifests
func (e *Env) createPrometheus(namespace string) {}

// Upload metrics tar to prometheus
func (e *Env) uploadMetrics(namespace string, metricsTarUrl string) {}

// Get prometheus route URL
func (e *Env) getPromRoute(namespace string) {}
