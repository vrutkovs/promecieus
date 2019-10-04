package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strings"
)

const (
	prowPrefix = "https://prow.svc.ci.openshift.org/view/"
	gcsPrefix  = "https://gcsweb-ci.svc.ci.openshift.org/"
)

// Env holds references to useful objects in router funcs
type Env struct{}

func (e *Env) create(c *gin.Context) {
	url := c.PostForm("url")
	log.Printf("url: %s", url)

	// Get artifacts link
	artifactsUrl := strings.Replace(url, prowPrefix, gcsPrefix, -1)
	log.Printf("artifacts: %s", artifactsUrl)

	// Get a link to prometheus metadata
	// TODO: aws is hardcoded :/
	metricsTar := fmt.Sprintf("%s/artifacts/e2e-aws/metrics/", artifactsUrl)
	log.Printf("metricsTar: %s", metricsTar)

	// Create namespace
	ns := e.generateNamespace()
	err := e.createPrometheus(ns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": err.Error(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("foo %s %s", metricsTar, ns),
	})
}

func main() {
	// Get apps domain from the route

	// setup webhook listener
	r := gin.Default()

	env := &Env{}

	// create prometheus instance
	r.POST("/create", env.create)

	r.Run(":8080")
}
