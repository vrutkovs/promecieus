package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
	"k8s.io/klog/v2"

	"github.com/vrutkovs/promecieus/pkg/promecieus"
)

// health is k8s endpoint for liveness check
func health(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func main() {
	kubeConfigEnvVar := os.Getenv("KUBECONFIG")
	klog.InitFlags(nil)

	k8sC, routeC, err := promecieus.TryLogin(kubeConfigEnvVar)
	if err != nil {
		klog.Fatalf("Failed to login in cluster: %v", err)
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

	rqStatus := promecieus.RQuotaStatus{}

	grafana := promecieus.GrafanaSettings{
		URL:    os.Getenv("GRAFANA_URL"),
		Token:  os.Getenv("GRAFANA_TOKEN"),
		Cookie: os.Getenv("GRAFANA_COOKIE"),
	}

	server := &promecieus.ServerSettings{
		K8sClient:   k8sC,
		RouteClient: routeC,
		Namespace:   namespace,
		RQuotaName:  rquotaName,
		RQStatus:    &rqStatus,
		Conns:       &promecieus.OpenSockets{},
		Datasources: make(map[string]int),
		Grafana:     &grafana,
	}

	ctx := context.Background()
	if err := server.GetResourceQuota(ctx); err != nil {
		klog.Fatalf("Failed to read initial resource quota: %v", err)
	} else {
		go server.WatchResourceQuota(ctx)
	}

	r := gin.New()
	r.SetTrustedProxies(nil)

	// Server static HTML
	r.Use(static.Serve("/", static.LocalFile("./html", true)))

	// Don't log k8s health endpoint
	r.Use(
		gin.LoggerWithWriter(gin.DefaultWriter, "/health"),
		gin.Recovery(),
	)
	r.GET("/health", health)
	r.GET("/ws/status", server.HandleStatusViaWS)

	go func() {
		gocron.Every(2).Minutes().Do(server.CleanupOldDeployements, ctx)
		<-gocron.Start()
	}()

	h2s := &http2.Server{}

	s := &http.Server{
		Addr:           ":8080",
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
		Handler:        h2c.NewHandler(r, h2s),
	}
	s.ListenAndServe()
}
