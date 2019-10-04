package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strings"
)

const (
	prowPrefix  = "https://prow.svc.ci.openshift.org/view/"
	gcsPrefix   = "https://gcsweb-ci.svc.ci.openshift.org/"
	promTarPath = "artifacts/e2e-aws/metrics/prometheus.tar"
)

func create(c *gin.Context) {
	url := c.PostForm("url")
	log.Printf("url: %s", url)

	// Get artifacts link
	artifactsUrl := strings.Replace(url, prowPrefix, gcsPrefix, -1)
	log.Printf("artifacts: %s", artifactsUrl)

	// Get a link to prometheus metadata
	// TODO: aws is hardcoded :/
	metricsTar := fmt.Sprintf("%s/%s", artifactsUrl, promTarPath)
	log.Printf("metricsTar: %s", metricsTar)

	// Create namespace
	ns := generateNamespace()
	err := createPrometheus(ns, metricsTar)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": err.Error(),
		})
		return
	}
	// TODO: Destroy namespace if error

	promRoute, err := getPromRoute(ns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": promRoute,
	})
}

func main() {
	r := gin.Default()
	// create prometheus instance
	r.POST("/create", create)

	r.Run(":8080")
}
