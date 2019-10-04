package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strings"
)

// Env holds references to useful objects in router funcs
type Env struct{}

func (e *Env) create(c *gin.Context) {
	url := c.PostForm("url")
	log.Println(fmt.Sprintf("url: %s", url))

	// Get artifacts link
	artifactsUrl := strings.ReplaceAll(url, "https://prow.svc.ci.openshift.org/view/", "https://gcsweb-ci.svc.ci.openshift.org")
	log.Println(fmt.Sprintf("artifacts: %s", artifactsUrl))

	// Get a link to prometheus metadata
	// TODO: aws is hardcoded :/
	metricsTar := fmt.Sprintf("%s/artifacts/e2e-aws/metrics/", artifactsUrl)
	log.Println(fmt.Sprintf("metricsTar: %s", metricsTar))

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("foo %s", url),
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
