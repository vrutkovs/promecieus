package main

import (
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
)

// health is k8s endpoint for liveness check
func health(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func main() {
	c, err := inClusterLogin()
	if err != nil {
		log.Println("Failed to login in cluster")
		log.Println(err)
		return
	}

	namespace := "promecieus"
	envVarNamespace := os.Getenv("NAMESPACE")
	if len(envVarNamespace) != 0 {
		namespace = envVarNamespace
	}

	server := &ServerSettings{k8sClient: c, namespace: namespace}
	r := gin.New()

	// Load templates from bin assets
	r.Use(static.Serve("/", static.LocalFile("./html", true)))

	// Don't log k8s health endpoint
	r.Use(
		gin.LoggerWithWriter(gin.DefaultWriter, "/health"),
		gin.Recovery(),
	)
	r.GET("/health", health)
	r.GET("/ws/status", server.handleStatusViaWS)

	r.Run(":8080")
}
