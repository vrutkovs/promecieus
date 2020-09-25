package promecieus

import (
	"time"

	"github.com/gorilla/websocket"
	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	k8s "k8s.io/client-go/kubernetes"
)

// GrafanaSettings stores grafana config
type GrafanaSettings struct {
	URL    string `json:"url"`
	Token  string `json:"token"`
	Cookie string `json:"cookie"`
}

// RQuotaStatus stores ResourceQuota info
type RQuotaStatus struct {
	Used int64 `json:"used"`
	Hard int64 `json:"hard"`
}

// ServerSettings stores info about the server
type ServerSettings struct {
	K8sClient   *k8s.Clientset
	RouteClient *routeClient.RouteV1Client
	Namespace   string
	RQuotaName  string
	RQStatus    *RQuotaStatus
	Conns       map[string]*websocket.Conn
	Datasources map[string]int
	Grafana     *GrafanaSettings
}

// ProwJSON stores test start / finished timestamp
type ProwJSON struct {
	Timestamp int `json:"timestamp"`
}

// ProwInfo stores all links and data collected via scanning for metrics
type ProwInfo struct {
	Started    time.Time
	Finished   time.Time
	MetricsURL string
}
