package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"golang.org/x/net/html"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	routeClient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	k8s "k8s.io/client-go/kubernetes"
)

// ServerSettings stores info about the server
type ServerSettings struct {
	statusWebSocket *websocket.Conn
	k8sClient       *k8s.Clientset
	routeClient     *routeClient.RouteV1Client
	namespace       string
}

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	prowPrefix    = "https://prow.svc.ci.openshift.org/view"
	gcsPrefix     = "https://gcsweb-ci.svc.ci.openshift.org"
	storagePrefix = "https://storage.googleapis.com"
	promTarPath   = "metrics/prometheus.tar"
)

func generateAppLabel() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, randLength)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func getGCSLinks(url string) ([]string, error) {
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

func getMetricsTar(baseUrl string) (string, error) {
	gcsTempUrl := strings.Replace(baseUrl, prowPrefix, gcsPrefix, -1)
	// Replace prow with gcs to get artifacts link
	gcsUrl, err := url.Parse(gcsTempUrl)
	if err != nil {
		return "", fmt.Errorf("Failed to parse GCS URL %s: %v", gcsTempUrl, err)
	}
	// Check that 'artifacts' folder is present
	gcsToplinks, err := getGCSLinks(gcsUrl.String())
	if err != nil {
		return "", fmt.Errorf("Failed to fetch top-level GCS link at %s: %v", gcsUrl, err)
	}
	if len(gcsToplinks) == 0 {
		return "", fmt.Errorf("No top-level GCS links at %s found", gcsUrl)
	}
	tmpArtifactsUrl := ""
	for _, link := range gcsToplinks {
		if strings.HasSuffix(link, "artifacts/") {
			tmpArtifactsUrl = gcsPrefix + link
			break
		}
	}
	if tmpArtifactsUrl == "" {
		return "", fmt.Errorf("Failed to find artifacts link in %v", gcsToplinks)
	}
	artifactsUrl, err := url.Parse(tmpArtifactsUrl)
	if err != nil {
		return "", fmt.Errorf("Failed to parse artifacts link %s: %v", tmpArtifactsUrl, err)
	}
	log.Printf("artifactsUrl: %s", artifactsUrl.String())

	// Get a list of folders in find ones which contain e2e
	artifactLinksToplinks, err := getGCSLinks(artifactsUrl.String())
	if err != nil {
		return "", fmt.Errorf("Failed to fetch artifacts link at %s: %v", gcsUrl, err)
	}
	if len(artifactLinksToplinks) == 0 {
		return "", fmt.Errorf("No artifact links at %s found", gcsUrl)
	}
	tmpE2eUrl := ""
	for _, link := range artifactLinksToplinks {
		if strings.Contains(link, "/e2e-") {
			tmpE2eUrl = gcsPrefix + link
		}
	}
	if tmpE2eUrl == "" {
		return "", fmt.Errorf("Failed to find e2e link in %v", artifactLinksToplinks)
	}
	e2eUrl, err := url.Parse(tmpE2eUrl)
	if err != nil {
		return "", fmt.Errorf("Failed to parse e2e link %s: %v", tmpE2eUrl, err)
	}
	log.Printf("e2eUrl: %s", e2eUrl.String())

	gcsMetricsURL := fmt.Sprintf("%s%s", e2eUrl.String(), promTarPath)
	tempMetricsURL := strings.Replace(gcsMetricsURL, gcsPrefix+"/gcs", storagePrefix, -1)
	expectedMetricsURL, err := url.Parse(tempMetricsURL)
	if err != nil {
		return "", fmt.Errorf("Failed to parse metrics link %s: %v", tempMetricsURL, err)
	}
	log.Printf("expectedMetricsURL: %s", expectedMetricsURL.String())
	// Check that metrics/prometheus.tar can be fetched and it non-null
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Head(expectedMetricsURL.String())
	if err != nil {
		return "", fmt.Errorf("Failed to fetch %s: %v", expectedMetricsURL.String(), err)
	}
	defer resp.Body.Close()

	contentLength := resp.Header.Get("content-length")
	if contentLength == "" {
		return "", fmt.Errorf("Failed to check arhive at %s: no content length returned", expectedMetricsURL)
	}
	length, err := strconv.Atoi(contentLength)
	if err != nil {
		return "", fmt.Errorf("Failed to check arhive at %s: %v", expectedMetricsURL, err)
	}
	if length == 0 {
		return "", fmt.Errorf("Failed to check arhive at %s: archive is empty", expectedMetricsURL)
	}
	return expectedMetricsURL.String(), nil
}

func updateKustomization(tmpDir string, metricsTar string, appLabel string) error {
	// Replace path to fetch metrics and set common labels
	deploymentPath := fmt.Sprintf("%s/%s", tmpDir, "kustomization.yaml")
	read, err := ioutil.ReadFile(deploymentPath)
	if err != nil {
		return err
	}
	newContents := strings.Replace(string(read), "PROMTAR_VALUE", metricsTar, -1)
	newContents = strings.Replace(newContents, "COMMON_LABEL", appLabel, -1)
	err = ioutil.WriteFile(deploymentPath, []byte(newContents), 0)
	if err != nil {
		return err
	}
	return nil
}

func applyKustomize(appLabel string, metricsTar string) (string, error) {
	// Make temp dir for assets
	tmpDir, err := ioutil.TempDir("", appLabel)
	defer os.RemoveAll(tmpDir)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	err = RestoreAssets(tmpDir, promTemplates)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	promTemplatesDir := fmt.Sprintf("%s/%s", tmpDir, promTemplates)
	err = updateKustomization(promTemplatesDir, metricsTar, appLabel)
	if err != nil {
		log.Printf(err.Error())
		return "", err
	}

	// Apply manifests via kustomize
	cmd := exec.Command("oc", "apply", "-k", promTemplatesDir)
	output, err := cmd.CombinedOutput()
	log.Printf(string(output))
	if err != nil {
		log.Printf(err.Error())
		return string(output), err
	}

	return string(output), nil
}
