package main

import (
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
	"log"
	"net/http"
	"os"
)

// health is k8s endpoint for liveness check
func health(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func main() {
	k8sC, routeC, err := inClusterLogin()
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

	server := &ServerSettings{k8sClient: k8sC, routeClient: routeC, namespace: namespace}
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

	go func() {
		gocron.Every(2).Minutes().Do(server.cleanupOldDeployements)
		<-gocron.Start()
	}()

	r.Run(":8080")
}
