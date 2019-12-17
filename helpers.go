package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"golang.org/x/net/html"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	k8s "k8s.io/client-go/kubernetes"
)

// RQuotaStatus stores ResourceQuota info
type RQuotaStatus struct {
	Used int64 `json:"used"`
	Hard int64 `json:"hard"`
}

// ServerSettings stores info about the server
type ServerSettings struct {
	k8sClient   *k8s.Clientset
	routeClient *routeClient.RouteV1Client
	namespace   string
	rquotaName  string
	rqStatus    RQuotaStatus
	conns       map[string]*websocket.Conn
}

// ProwJSON stores test start / finished timestamp
type ProwJSON struct {
	Timestamp int `json:"timestamp"`
}

// ProwLinks stores all links and data collected via scanning for metrics
type ProwInfo struct {
	Started    time.Time
	Finished   time.Time
	MetricsUrl string
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	prowPrefix    = "https://prow.svc.ci.openshift.org/view"
	gcsPrefix     = "https://gcsweb-ci.svc.ci.openshift.org"
	storagePrefix = "https://storage.googleapis.com"
	promTarPath   = "metrics/prometheus.tar"
	e2ePrefix     = "e2e"
)

func generateAppLabel() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func getLinksFromUrl(url string) ([]string, error) {
	links := []string{}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch %s: %v", url, err)
	}
	defer resp.Body.Close()

	z := html.NewTokenizer(resp.Body)
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done
			return links, nil
		case tt == html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" {
						links = append(links, a.Val)
						break
					}
				}
			}
		}
	}
}

func getMetricsTar(conn *websocket.Conn, url string) (ProwInfo, error) {
	prowInfo, err := getTarURLFromProw(url)
	if err != nil {
		return prowInfo, err
	}
	expectedMetricsURL := prowInfo.MetricsUrl

	sendWSMessage(conn, "status", fmt.Sprintf("Found prometheus archive at %s", expectedMetricsURL))

	// Check that metrics/prometheus.tar can be fetched and it non-null
	sendWSMessage(conn, "status", "Checking if prometheus archive can be fetched")
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Head(expectedMetricsURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch %s: %v", expectedMetricsURL, err)
	}
	defer resp.Body.Close()

	contentLength := resp.Header.Get("content-length")
	if contentLength == "" {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: no content length returned", expectedMetricsURL)
	}
	length, err := strconv.Atoi(contentLength)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: %v", expectedMetricsURL, err)
	}
	if length == 0 {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: archive is empty", expectedMetricsURL)
	}
	return prowInfo, nil
}

func getTarURLFromProw(baseUrl string) (ProwInfo, error) {
	prowInfo := ProwInfo{}

	gcsTempUrl := strings.Replace(baseUrl, prowPrefix, gcsPrefix, -1)
	// Replace prow with gcs to get artifacts link
	gcsUrl, err := url.Parse(gcsTempUrl)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse GCS URL %s: %v", gcsTempUrl, err)
	}

	// Fetch start and finish time of the test
	startTime, err := getTimeStampFromProwJson(fmt.Sprintf("%s/started.json", gcsUrl))
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch test start time: %v", err)
	}
	prowInfo.Started = startTime

	finishedTime, err := getTimeStampFromProwJson(fmt.Sprintf("%s/finished.json", gcsUrl))
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch test finshed time: %v", err)
	}
	prowInfo.Finished = finishedTime

	// Check that 'artifacts' folder is present
	gcsToplinks, err := getLinksFromUrl(gcsUrl.String())
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch top-level GCS link at %s: %v", gcsUrl, err)
	}
	if len(gcsToplinks) == 0 {
		return prowInfo, fmt.Errorf("No top-level GCS links at %s found", gcsUrl)
	}
	tmpArtifactsUrl := ""
	for _, link := range gcsToplinks {
		if strings.HasSuffix(link, "artifacts/") {
			tmpArtifactsUrl = gcsPrefix + link
			break
		}
	}
	if tmpArtifactsUrl == "" {
		return prowInfo, fmt.Errorf("Failed to find artifacts link in %v", gcsToplinks)
	}
	artifactsUrl, err := url.Parse(tmpArtifactsUrl)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse artifacts link %s: %v", tmpArtifactsUrl, err)
	}

	// Get a list of folders in find ones which contain e2e
	artifactLinksToplinks, err := getLinksFromUrl(artifactsUrl.String())
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch artifacts link at %s: %v", gcsUrl, err)
	}
	if len(artifactLinksToplinks) == 0 {
		return prowInfo, fmt.Errorf("No artifact links at %s found", gcsUrl)
	}
	tmpE2eUrl := ""
	for _, link := range artifactLinksToplinks {
		log.Printf("link: %s", link)
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		log.Printf("lastPathSection: %s", lastPathSegment)
		if strings.Contains(lastPathSegment, e2ePrefix) {
			tmpE2eUrl = gcsPrefix + link
		}
	}
	if tmpE2eUrl == "" {
		return prowInfo, fmt.Errorf("Failed to find e2e link in %v", artifactLinksToplinks)
	}
	e2eUrl, err := url.Parse(tmpE2eUrl)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse e2e link %s: %v", tmpE2eUrl, err)
	}
	gcsMetricsURL := fmt.Sprintf("%s%s", e2eUrl.String(), promTarPath)
	tempMetricsURL := strings.Replace(gcsMetricsURL, gcsPrefix+"/gcs", storagePrefix, -1)
	expectedMetricsURL, err := url.Parse(tempMetricsURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse metrics link %s: %v", tempMetricsURL, err)
	}
	prowInfo.MetricsUrl = expectedMetricsURL.String()
	return prowInfo, nil
}

func getTimeStampFromProwJson(rawUrl string) (time.Time, error) {
	jsonUrl, err := url.Parse(rawUrl)
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to fetch prow JSOM at %s: %v", rawUrl, err)
	}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Get(jsonUrl.String())
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to fetch %s: %v", jsonUrl.String(), err)
	}
	defer resp.Body.Close()

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		return time.Now(), fmt.Errorf("Failed to read body at %s: %v", jsonUrl.String(), err)
	}

	var prowInfo ProwJSON
	err = json.Unmarshal(body, &prowInfo)
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to unmarshal json %s: %v", body, err)
	}

	return time.Unix(int64(prowInfo.Timestamp), 0), nil
}
