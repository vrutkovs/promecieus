package promecieus

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/html"
)

const (
	charset       = "abcdefghijklmnopqrstuvwxyz"
	randLength    = 8
	promTemplates = "prom-templates"
	gcsLinkToken  = "gcsweb"
	gcsPrefix     = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com"
	storagePrefix = "https://storage.googleapis.com"
	promTarPath   = "metrics/prometheus.tar"
	extraPath     = "gather-extra"
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

func getLinksFromURL(url string) ([]string, error) {
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

func ensureMetricsURL(url string) (int, error) {
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Head(url)
	if resp == nil {
		return 0, err
	}
	return resp.StatusCode, err
}

func getMetricsTar(conn *websocket.Conn, url string) (ProwInfo, error) {
	sendWSMessage(conn, "status", fmt.Sprintf("Fetching %s", url))
	// Ensure initial URL is valid
	statusCode, err := ensureMetricsURL(url)
	if err != nil || statusCode != http.StatusOK {
		return ProwInfo{}, fmt.Errorf("Failed to fetch url %s: code %d, %s", url, statusCode, err)
	}

	prowInfo, err := getTarURLFromProw(conn, url)
	if err != nil {
		return prowInfo, err
	}
	expectedMetricsURL := prowInfo.MetricsURL

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

	if resp.StatusCode != 200 {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: returned %s", expectedMetricsURL, resp.Status)
	}

	contentLength := resp.Header.Get("content-length")
	if contentLength == "" {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: no content length returned", expectedMetricsURL)
	}
	length, err := strconv.Atoi(contentLength)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: invalid content-length: %v", expectedMetricsURL, err)
	}
	if length == 0 {
		return prowInfo, fmt.Errorf("Failed to check archive at %s: archive is empty", expectedMetricsURL)
	}
	return prowInfo, nil
}

func getTarURLFromProw(conn *websocket.Conn, baseURL string) (ProwInfo, error) {
	prowInfo := ProwInfo{}

	// Is it a direct prom tarball link?
	if strings.HasSuffix(baseURL, promTarPath) {
		prowInfo.MetricsURL = baseURL
		// there is no way to find out the time via direct tarball link, use current time
		prowInfo.Finished = time.Now()
		prowInfo.Started = time.Now()
		return prowInfo, nil
	}

	// Get a list of links on prow page
	prowToplinks, err := getLinksFromURL(baseURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to find links at %s: %v", prowToplinks, err)
	}
	if len(prowToplinks) == 0 {
		return prowInfo, fmt.Errorf("No links found at %s", baseURL)
	}
	gcsTempURL := ""
	for _, link := range prowToplinks {
		log.Printf("link: %s", link)
		if strings.Contains(link, gcsLinkToken) {
			gcsTempURL = link
			break
		}
	}
	if gcsTempURL == "" {
		return prowInfo, fmt.Errorf("Failed to find GCS link in %v", prowToplinks)
	}
	sendWSMessage(conn, "status", fmt.Sprintf("Found gcs link at %s", baseURL))

	gcsURL, err := url.Parse(gcsTempURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse GCS URL %s: %v", gcsTempURL, err)
	}

	// Fetch start and finish time of the test
	startTime, err := getTimeStampFromProwJSON(fmt.Sprintf("%s/started.json", gcsURL))
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch test start time: %v", err)
	}
	prowInfo.Started = startTime

	finishedTime, err := getTimeStampFromProwJSON(fmt.Sprintf("%s/finished.json", gcsURL))
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch test finshed time: %v", err)
	}
	prowInfo.Finished = finishedTime

	sendWSMessage(conn, "status", fmt.Sprintf("Found start/stop markers at %s", gcsURL))

	// Check that 'artifacts' folder is present
	gcsToplinks, err := getLinksFromURL(gcsURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch top-level GCS link at %s: %v", gcsURL, err)
	}
	if len(gcsToplinks) == 0 {
		return prowInfo, fmt.Errorf("No top-level GCS links at %s found", gcsURL)
	}
	tmpArtifactsURL := ""
	for _, link := range gcsToplinks {
		if strings.HasSuffix(link, "artifacts/") {
			tmpArtifactsURL = gcsPrefix + link
			break
		}
	}
	if tmpArtifactsURL == "" {
		return prowInfo, fmt.Errorf("Failed to find artifacts link in %v", gcsToplinks)
	}
	artifactsURL, err := url.Parse(tmpArtifactsURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse artifacts link %s: %v", tmpArtifactsURL, err)
	}

	// Get a list of folders in find ones which contain e2e
	artifactLinksToplinks, err := getLinksFromURL(artifactsURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch artifacts link at %s: %v", gcsURL, err)
	}
	if len(artifactLinksToplinks) == 0 {
		return prowInfo, fmt.Errorf("No artifact links at %s found", gcsURL)
	}
	tmpE2eURL := ""
	for _, link := range artifactLinksToplinks {
		log.Printf("link: %s", link)
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		log.Printf("lastPathSection: %s", lastPathSegment)
		if strings.Contains(lastPathSegment, e2ePrefix) {
			tmpE2eURL = gcsPrefix + link
			break
		}
	}
	if tmpE2eURL == "" {
		return prowInfo, fmt.Errorf("Failed to find e2e link in %v", artifactLinksToplinks)
	}
	e2eURL, err := url.Parse(tmpE2eURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse e2e link %s: %v", tmpE2eURL, err)
	}

	// Support new-style jobs
	e2eToplinks, err := getLinksFromURL(e2eURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to fetch artifacts link at %s: %v", e2eURL, err)
	}
	if len(e2eToplinks) == 0 {
		return prowInfo, fmt.Errorf("No top links at %s found", e2eURL)
	}
	for _, link := range e2eToplinks {
		log.Printf("link: %s", link)
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		log.Printf("lastPathSection: %s", lastPathSegment)
		if lastPathSegment == extraPath {
			tmpE2eURL = gcsPrefix + link
			e2eURL, err = url.Parse(tmpE2eURL)
			if err != nil {
				return prowInfo, fmt.Errorf("Failed to parse e2e link %s: %v", tmpE2eURL, err)
			}
			break
		}
	}

	gcsMetricsURL := fmt.Sprintf("%s%s", e2eURL.String(), promTarPath)
	tempMetricsURL := strings.Replace(gcsMetricsURL, gcsPrefix+"/gcs", storagePrefix, -1)
	expectedMetricsURL, err := url.Parse(tempMetricsURL)
	if err != nil {
		return prowInfo, fmt.Errorf("Failed to parse metrics link %s: %v", tempMetricsURL, err)
	}
	prowInfo.MetricsURL = expectedMetricsURL.String()
	return prowInfo, nil
}

func getTimeStampFromProwJSON(rawURL string) (time.Time, error) {
	jsonURL, err := url.Parse(rawURL)
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to fetch prow JSOM at %s: %v", rawURL, err)
	}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Get(jsonURL.String())
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to fetch %s: %v", jsonURL.String(), err)
	}
	defer resp.Body.Close()

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		return time.Now(), fmt.Errorf("Failed to read body at %s: %v", jsonURL.String(), err)
	}

	var prowInfo ProwJSON
	err = json.Unmarshal(body, &prowInfo)
	if err != nil {
		return time.Now(), fmt.Errorf("Failed to unmarshal json %s: %v", body, err)
	}

	return time.Unix(int64(prowInfo.Timestamp), 0), nil
}
