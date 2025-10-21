# PromeCIeus

PromeCIeus is a web-based tool for exploring and analyzing Prometheus metrics from OpenShift CI/CD test runs. It provides a convenient interface to launch temporary Prometheus instances with metrics from test failures, allowing developers to debug performance issues and analyze test results.

## Features

- **Web Interface**: React-based UI for browsing and selecting test metrics
- **Dynamic Prometheus Deployment**: Automatically deploys temporary Prometheus instances with selected metrics
- **Kubernetes Integration**: Seamlessly works with OpenShift and Kubernetes clusters
- **Real-time Updates**: WebSocket-based status updates during deployment and analysis
- **Resource Management**: Automatic cleanup of old deployments and resource quota monitoring
- **Grafana Integration**: Optional integration with Grafana for enhanced visualization

## Prerequisites

- Kubernetes or OpenShift cluster (v1.24+)
- Go 1.23+
- Docker (for building)
- Appropriate RBAC permissions to deploy applications in the target namespace

## Installation

### From Source

1. Clone the repository:
```bash
git clone https://github.com/vrutkovs/promecieus.git
cd promecieus
```

2. Build the application:
```bash
go mod vendor
go build -o promecieus ./cmd/promecieus
```

### Using Docker

1. Build the Docker image:
```bash
docker build -t promecieus .
```

2. Run the container:
```bash
docker run -p 8080:8080 promecieus
```

### Deploying to OpenShift

Use the provided manifests:

```bash
oc apply -f manifests/
```

This will create:
- Namespace `promecieus`
- BuildConfig configuration
- DeploymentConfig configuration
- Service and route
- Service account with appropriate permissions
- Resource quota

## Configuration

PromeCIeus can be configured via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `KUBECONFIG` | - | Path to kubeconfig file (if not running in-cluster) |
| `NAMESPACE` | `promecieus` | Target namespace for deployments |
| `QUOTA_NAME` | `pod-quota` | Name of the ResourceQuota to monitor |
| `GRAFANA_URL` | - | Grafana instance URL (optional) |
| `GRAFANA_TOKEN` | - | Grafana API token (optional) |
| `GRAFANA_COOKIE` | - | Grafana authentication cookie (optional) |

## Usage

1. **Start the application**:
   ```bash
   ./promecieus
   ```

   The server will start on port 8080 and serve the web interface.

2. **Access the web interface**:
   Open your browser and navigate to `http://localhost:8080`

3. **Provide metrics URL**:
   Enter the URL to a Prometheus metrics archive (typically from CI test runs)

4. **Launch Prometheus**:
   Click the launch button to deploy a temporary Prometheus instance with the selected metrics

4. **Analyze metrics**:
   Once deployed, you can access the Prometheus UI to query and visualize the metrics

## Update Procedure

To update the PromeCIeus deployment in OpenShift:

### Using BuildConfig

1. **Trigger a new build**:
   ```bash
   oc start-build promecieus -n promecieus
   ```

2. **Monitor the build**:
   ```bash
   oc get builds -n promecieus -w
   ```

3. **Check the deployment**:
   ```bash
   oc get deploymentconfig/promecieus -n promecieus
   ```

The BuildConfig is configured to:
- Build from the `master` branch of the GitHub repository
- Automatically trigger builds when the base image changes
- Support GitHub webhooks for automatic builds on code changes
- Keep the last 5 successful and failed builds for troubleshooting

## How It Works

1. **Metrics Discovery**: PromeCIeus accepts URLs to CI test artifacts containing Prometheus metrics
2. **Archive Processing**: It extracts and validates the metrics archive
3. **Dynamic Deployment**: Creates a temporary Kubernetes deployment with:
   - An init container that downloads and extracts the metrics
   - A Prometheus server container that loads the extracted metrics
   - Appropriate service and route for access
4. **Resource Management**: Monitors resource quotas and automatically cleans up deployments after 4 hours
5. **Real-time Updates**: Provides WebSocket-based status updates throughout the deployment process

## Supported Metrics Archive Formats

PromeCIeus supports multiple Prometheus metrics archive formats and URL types:

### Primary Archive Formats

1. **Standard Prometheus Archive** (`metrics/prometheus.tar`)
   - Default format for most CI test runs
   - Contains the complete Prometheus metrics snapshot

2. **Alternative Prometheus Archive** (`metrics/prometheus-k8s-1.tar`)
   - Used for alternative snapshot formats
   - Can be accessed by adding `?altsnap=true` to the URL

### URL Types Supported

1. **Direct Archive URLs**
   - Direct links to `metrics/prometheus.tar` or `metrics/prometheus-k8s-1.tar`
   - Example: `https://storage.googleapis.com/path/to/metrics/prometheus.tar`

2. **Prow CI Job URLs**
   - Prow job result pages (e.g., `https://prow.ci.openshift.org/view/gs/origin-ci-test/...`)
   - PromeCIeus automatically parses the page to find the metrics archive
   - Supports both GCS web URLs and direct storage URLs

3. **GCS Web Interface URLs**
   - URLs from GCS web interface (`https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/...`)
   - Automatically converted to direct storage URLs for download

### Alternative Metrics Selection

To use the alternative Prometheus metrics archive (`prometheus-k8s-1.tar`), add the `altsnap` query parameter to your URL:

```
https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/periodic-ci-openshift-release-master-ci-4.15-e2e-aws-upgrade/1723456789?altsnap=true
```

This tells PromeCIeus to look for `metrics/prometheus-k8s-1.tar` instead of the default `metrics/prometheus.tar`.

### Archive Validation

PromeCIeus performs several validation checks:
- Verifies the archive is accessible via HTTP HEAD request
- Ensures the archive is not empty (content-length > 0)
- Checks that the archive can be downloaded and extracted
- Validates the extracted metrics data format

### Custom Prometheus Image

PromeCIeus uses a custom Prometheus image (`quay.io/amrini/prometheus:v3.0.1-loosen`) that includes modifications to handle potentially corrupted WAL records that may occur during CI test failures.

## Development

### Project Structure

```
promecieus/
├── cmd/promecieus/          # Main application entry point
├── pkg/promecieus/          # Core application logic
│   ├── k8s.go              # Kubernetes client operations
│   ├── websocket.go        # WebSocket handling
│   ├── helpers.go          # Utility functions
│   └── types.go            # Type definitions
├── html/                   # Frontend assets
│   ├── index.html          # Main HTML page
│   ├── app.jsx            # React application
│   └── storage.js         # Browser storage utilities
├── manifests/              # Kubernetes/OpenShift manifests
└── Dockerfile             # Container build definition
```

### Running in Development

1. Install dependencies:
```bash
go mod download
```

2. Run the application:
```bash
go run ./cmd/promecieus
```

The web interface will be available at `http://localhost:8080`

### Testing

Run the test suite:
```bash
go test ./...
```

## Security Considerations

- PromeCIeus requires service account permissions to deploy applications in the target namespace
- The application processes external URLs and downloads archives - ensure proper network security
- Resource quotas are enforced to prevent resource exhaustion
- Deployments are automatically cleaned up to prevent resource leaks

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

## Support

For issues and questions:
- Create an issue on GitHub: https://github.com/vrutkovs/promecieus/issues
- Check the troubleshooting section below

## Troubleshooting

### Common Issues

**Deployment fails with "insufficient quota"**
- Check the ResourceQuota configuration in your namespace
- Verify that the quota name matches the `QUOTA_NAME` environment variable

**Prometheus fails to start**
- Check the deployment logs for error messages
- Verify that the metrics archive is accessible and valid
- Ensure the Prometheus image can be pulled from the registry

**WebSocket connection issues**
- Verify that the firewall allows WebSocket connections
- Check that the service is properly configured

**Metrics archive not found**
- Ensure the URL is accessible and points to a valid Prometheus metrics archive
- Check that the archive contains the expected `metrics/prometheus.tar` or `metrics/prometheus-k8s-1.tar` files
- Verify the URL format matches the supported types (direct archive, Prow job, or GCS web interface)
- If using alternative metrics, ensure `?altsnap=true` is added to the URL

**Archive download fails**
- Check network connectivity to the storage bucket
- Verify the archive URL is publicly accessible or that proper authentication is configured
- Ensure the storage bucket permissions allow read access
- Check if the archive file exists and is not corrupted

**Alternative metrics not loading**
- Verify the `altsnap=true` query parameter is properly added to the URL
- Check that the `metrics/prometheus-k8s-1.tar` file exists in the artifact location
- Ensure the alternative archive format is valid and contains Prometheus data
