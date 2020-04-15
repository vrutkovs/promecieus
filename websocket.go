package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WSMessage represents websocket message format
type WSMessage struct {
	Message string `json:"message"`
	Action  string `json:"action"`
}

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func sendWSMessage(conn *websocket.Conn, action string, message string) {
	response := WSMessage{
		Action:  action,
		Message: message,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		fmt.Println("Can't serialize", response)
	}
	if conn != nil {
		conn.WriteMessage(websocket.TextMessage, responseJSON)
	}
}

func (s *ServerSettings) handleStatusViaWS(c *gin.Context) {
	conn, err := wsupgrader.Upgrade(c.Writer, c.Request, nil)

	if err != nil {
		log.Printf("Failed to upgrade ws: %+v", err)
		return
	}

	for {
		t, msg, err := conn.ReadMessage()
		log.Printf("Got ws message: %s", msg)
		if err != nil {
			if !websocket.IsCloseError(err, 1001, 1006) {
				delete(s.conns, conn.RemoteAddr().String())
				log.Printf("Error reading message: %+v", err)
			}
			break
		}
		if t != websocket.TextMessage {
			log.Printf("Not a text message: %d", t)
			continue
		}
		var m WSMessage
		err = json.Unmarshal(msg, &m)
		if err != nil {
			log.Printf("Failed to unmarshal message '%+v': %+v", string(msg), err)
			continue
		}
		log.Printf("WS message: %+v", m)
		switch m.Action {
		case "connect":
			s.conns[conn.RemoteAddr().String()] = conn
			go s.sendResourceQuotaUpdate()
		case "new":
			go s.createNewPrometheus(conn, m.Message)
		case "delete":
			go s.removeProm(conn, m.Message)
		}
	}
}

func (s *ServerSettings) sendResourceQuotaUpdate() {
	rqsJSON, err := json.Marshal(s.rqStatus)
	if err != nil {
		log.Fatalf("Can't serialize %s", err)
	}
	for _, conn := range s.conns {
		sendWSMessage(conn, "rquota", string(rqsJSON))
	}
}

func (s *ServerSettings) removeProm(conn *websocket.Conn, appName string) {
	sendWSMessage(conn, "status", fmt.Sprintf("Removing app %s", appName))
	if output, err := s.deletePods(appName); err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("%s\n%s", output, err.Error()))
		return
	}
	sendWSMessage(conn, "done", "Prometheus instance removed")
}

func (s *ServerSettings) createNewPrometheus(conn *websocket.Conn, rawURL string) {
	// Generate a unique app label
	appLabel := generateAppLabel()
	sendWSMessage(conn, "app-label", appLabel)

	// Fetch metrics.tar path if prow URL specified
	prowInfo, err := getMetricsTar(conn, rawURL)
	if err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("Failed to find metrics archive: %s", err.Error()))
		return
	}

	// Create a new app in the namespace and return route
	sendWSMessage(conn, "status", "Deploying a new prometheus instance")

	var promRoute string
	metricsTar := prowInfo.MetricsURL
	if promRoute, err = s.launchPromApp(appLabel, metricsTar); err != nil {
		sendWSMessage(conn, "failure", fmt.Sprintf("Failed to run a new app: %s", err.Error()))
		return
	}
	// Calculate a range in minutes between start and finish
	elapsed := int(prowInfo.Finished.Sub(prowInfo.Started).Minutes())
	finishedDate := url.QueryEscape(prowInfo.Finished.Format("2006-01-02 15:04"))

	// Send a sample query so that user would not have to rediscover start and finished time
	query := fmt.Sprintf("g0.range_input=%dm&g0.end_input=%s&g0.expr=up&g0.tab=0", elapsed, finishedDate)
	sampleQuery := fmt.Sprintf("%s/graph?%s", promRoute, query)

	sendWSMessage(conn, "link", sampleQuery)
	sendWSMessage(conn, "progress", "Waiting for pods to become ready")
	if err := s.waitForDeploymentReady(appLabel); err != nil {
		sendWSMessage(conn, "failure", err.Error())
		return
	}
	sendWSMessage(conn, "done", "Pod is ready")
}
