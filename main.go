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

	// Get artifacts link
	artifactsUrl := strings.Replace(url, prowPrefix, gcsPrefix, -1)

	// Get a link to prometheus metadata
	// TODO: aws is hardcoded :/
	metricsTar := fmt.Sprintf("%s/%s", artifactsUrl, promTarPath)
	log.Printf("metricsTar: %s", metricsTar)

	// Create namespace
	appLabel := generateAppLabel()
	log.Printf("Generating app %s", appLabel)
	err := createPrometheus(appLabel, metricsTar)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": err.Error(),
		})
		return
	}

	// Return route name
	promRoute, err := getPromRoute(appLabel)
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
