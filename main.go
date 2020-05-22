package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jasonlvhit/gocron"
)

// health is k8s endpoint for liveness check
func health(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func main() {
	kubeConfigEnvVar := os.Getenv("KUBECONFIG")

	k8sC, routeC, err := tryLogin(kubeConfigEnvVar)
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

	rquotaName := "pod-quota"
	envVarRquotaName := os.Getenv("QUOTA_NAME")
	if len(envVarRquotaName) != 0 {
		rquotaName = envVarRquotaName
	}

	rqStatus := RQuotaStatus{}

	server := &ServerSettings{
		k8sClient:   k8sC,
		routeClient: routeC,
		namespace:   namespace,
		rquotaName:  rquotaName,
		rqStatus:    rqStatus,
		conns:       make(map[string]*websocket.Conn),
	}
	if server.getResourceQuota() != nil {
		panic("Failed to read initial resource quota")
	}
	go server.watchResourceQuota()

	r := gin.New()

	// Server static HTML
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
