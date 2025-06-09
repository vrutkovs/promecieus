package promecieus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"k8s.io/klog/v2"
)

// WSMessage represents websocket message format
type WSMessage struct {
	Message string            `json:"message"`
	Action  string            `json:"action"`
	Data    map[string]string `json:"data,omitempty"`
}

type ContainerLogError struct {
	Header  string `json:"header"`
	Message string `json:"message"`
}

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections
		return true
	},
}

func sendWSMessage(conn *websocket.Conn, action string, message string) {
	response := WSMessage{
		Action:  action,
		Message: message,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		klog.Fatalf("Can't serialize %v", response)
	}
	if conn != nil {
		conn.WriteMessage(websocket.TextMessage, responseJSON)
	}
}

// HandleStatusViaWS reads websocket events and runs actions
func (s *ServerSettings) HandleStatusViaWS(c *gin.Context) {
	conn, err := wsupgrader.Upgrade(c.Writer, c.Request, nil)

	if err != nil {
		klog.Warningf("Failed to upgrade ws: %+v", err)
		return
	}

	ctx := context.Background()

	for {
		t, msg, err := conn.ReadMessage()
		klog.Infof("Got ws message: %s", msg)
		if err != nil {
			if !websocket.IsCloseError(err, 1001, 1006) {
				s.RemoveWS(conn)
				klog.Warningf("Error reading message: %+v", err)
			}
			break
		}
		if t != websocket.TextMessage {
			klog.Warningf("Not a text message: %d", t)
			continue
		}
		var m WSMessage
		err = json.Unmarshal(msg, &m)
		if err != nil {
			klog.Warningf("Failed to unmarshal message '%+v': %+v", string(msg), err)
			continue
		}
		klog.Infof("WS message: %+v", m)
		switch m.Action {
		case "connect":
			s.AddOrUpdateWS(conn)
			go s.sendResourceQuotaUpdate()
		case "new":
			go s.createNewPrometheus(ctx, conn, m.Message)
		case "delete":
			go s.removeProm(ctx, conn, m.Message)
		}
	}
}

func (s *ServerSettings) AddOrUpdateWS(conn *websocket.Conn) {
	s.Conns.Lock()
	defer s.Conns.Unlock()
	if s.Conns.list == nil {
		s.Conns.list = make(map[string]*websocket.Conn)
	}
	s.Conns.list[conn.RemoteAddr().String()] = conn
}

func (s *ServerSettings) RemoveWS(conn *websocket.Conn) {
	s.Conns.Lock()
	defer s.Conns.Unlock()
	delete(s.Conns.list, conn.RemoteAddr().String())
}

func (s *ServerSettings) sendResourceQuotaUpdate() {
	rqsJSON, err := json.Marshal(s.RQStatus)
	if err != nil {
		klog.Fatalf("Can't serialize %s", err)
	}
	klog.Infof("Sending RQuota update to : %#v", s.Conns)
	for _, conn := range s.Conns.list {
		sendWSMessage(conn, "rquota", string(rqsJSON))
	}
}

func (s *ServerSettings) removeProm(ctx context.Context, conn *websocket.Conn, appName string) {
	sendWSMessage(conn, "status", fmt.Sprintf("Removing app %s", appName))
	if output, err := s.deletePods(ctx, appName); err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("%s\n%s", output, err.Error()))
		return
	}
	dsID := s.Datasources[appName]
	if err := s.removeDataSource(dsID); err != nil {
		sendWSMessage(conn, "failure", err.Error())
	}
	delete(s.Datasources, appName)
	sendWSMessage(conn, "done", "Prometheus instance removed")
}

func (s *ServerSettings) createNewPrometheus(ctx context.Context, conn *websocket.Conn, rawURL string) {
	// Generate a unique app label
	appLabel := generateAppLabel()
	sendWSMessage(conn, "app-label", appLabel)

	// Fetch metrics.tar path if prow URL specified
	u, err := url.Parse(rawURL)
	if err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("Failed to parse url: %s", err.Error()))
		return
	}

	prowInfo, err := getMetricsTar(conn, u)
	if err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("Failed to find metrics archive: %s", err.Error()))
		return
	}

	// Create a new app in the namespace and return route
	sendWSMessage(conn, "status", "Deploying a new prometheus instance")

	var promRoute string
	metricsTar := prowInfo.MetricsURL
	if promRoute, err = s.launchPromApp(ctx, appLabel, metricsTar); err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("Failed to run a new app: %s", err.Error()))
		return
	}
	// Calculate a range in minutes between start and finish
	elapsed := prowInfo.Finished.Sub(prowInfo.Started)

	// Send a sample query so that user would not have to rediscover start and finished time
	prometheusURL, err := url.Parse(promRoute)
	if err != nil {
		sendWSMessage(conn, "failure", err.Error())
		return
	}

	params := url.Values{}
	// expr has to be first param
	// params.Add("g0.expr", "up")
	params.Add("g0.tab", "0")
	params.Add("g0.stacked", "0")
	params.Add("g0.range_input", elapsed.String())
	params.Add("g0.end_input", prowInfo.Finished.Format("2006-01-02 15:04"))
	// prometheusURL.Path += "graph/g0.expr=up"
	prometheusURL.RawQuery = params.Encode()

	//sendWSMessage(conn, "link", prometheusURL.String())
	hackedPrometheusURL := fmt.Sprintf("%s/graph?g0.expr=up&%s", promRoute, params.Encode())
	sendWSMessage(conn, "link", hackedPrometheusURL)

	sendWSMessage(conn, "progress", "Waiting for pods to be created")
	if err := s.waitForDeploymentReady(ctx, appLabel, conn); err != nil {
		if errors.Is(err, ErrorContainerLog) {
			sendWSMessage(conn, "error", err.Error())
		} else {
			sendWSMessage(conn, "failure", err.Error())
		}
		return
	}

	if err := s.waitForEndpointReady(ctx, promRoute); err != nil {
		sendWSMessage(conn, "failure", err.Error())
		return
	}

	if s.Grafana.URL != "" && s.Grafana.Token != "" && s.Grafana.Cookie != "" {
		dsID, err := s.addDataSource(appLabel, promRoute)
		if err == nil {
			s.Datasources[appLabel] = dsID
			sendWSMessage(conn, "status", fmt.Sprintf("Added %s datasource at %s", appLabel, s.Grafana.URL))
		} else {
			sendWSMessage(conn, "failure", err.Error())
		}
	}
	sendWSMessage(conn, "done", "Pod is ready")
}

// GrafanaDatasource represents a datasource to be created
type GrafanaDatasource struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Access    string `json:"access"`
	BasicAuth bool   `json:"basicAuth"`
}

// GrafanaDatasourceResponse represents response from grafana
type GrafanaDatasourceResponse struct {
	DataSource struct {
		ID int `json:"id"`
	} `json:"datasource"`
}

func (s *ServerSettings) grafanaRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Grafana.Token))
	req.Header.Set("Cookie", s.Grafana.Cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (s *ServerSettings) addDataSource(appLabel, promRoute string) (int, error) {
	klog.Infof("adding %s as grafana datasource", promRoute)
	ds := &GrafanaDatasource{
		Name:      appLabel,
		URL:       promRoute,
		Type:      "prometheus",
		Access:    "proxy",
		BasicAuth: false,
	}
	data, err := json.Marshal(ds)
	if err != nil {
		return 0, nil
	}
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	apiURL := fmt.Sprintf("%s/api/datasources", s.Grafana.URL)
	req, err := s.grafanaRequest("POST", apiURL, bytes.NewBuffer(data))
	if err != nil {
		return 0, fmt.Errorf("failed to construct POST request to %s: %v", apiURL, err)
	}
	resp, err := netClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to perform request %s: %v", apiURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read body: %v", err)
	}
	dsResponse := &GrafanaDatasourceResponse{}
	if err := json.Unmarshal(body, dsResponse); err != nil {
		return 0, fmt.Errorf("failed to unmarshal response %s : %v", body, err)
	}
	return dsResponse.DataSource.ID, nil
}

func (s *ServerSettings) removeDataSource(id int) error {
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	apiURL := fmt.Sprintf("%s/api/datasources/%d", s.Grafana.URL, id)
	req, err := s.grafanaRequest("DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to construct DELETE request to %s: %v", apiURL, err)
	}
	resp, err := netClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform request %s: %v", apiURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %v", err)
	}
	dsResponse := &GrafanaDatasourceResponse{}
	if err := json.Unmarshal(body, dsResponse); err != nil {
		return fmt.Errorf("failed to unmarshal response %s : %v", body, err)
	}
	return nil

}
