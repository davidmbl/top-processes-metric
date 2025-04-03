# Top Process Exporter

A self-contained Prometheus exporter that collects information about top CPU and memory consuming processes and exposes them in Prometheus format.

## Features

- **Standalone**: No external dependencies (like cron, node_exporter, etc.)
- **Prometheus-ready**: Exposes metrics in Prometheus format at `/metrics` endpoint
- **Docker-based**: Easy to deploy with Docker or Docker Compose
- **Resource-efficient**: Caches results to reduce system impact
- **Detailed metrics**: Provides extensive process information including:
  - CPU usage percentage
  - Memory usage (percentage and bytes)
  - Process uptime
  - Thread count
  - CPU time (user and system)
  - User information

## Quick Start

### Using Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/your-username/top-process-exporter.git
cd top-process-exporter

# Start the exporter
docker-compose up -d
```

The exporter will be available at http://localhost:9256/metrics

### Using Docker Directly

```bash
# Build the image
docker build -t top-process-exporter .

# Run the container
docker run -d \
  --name top-process-exporter \
  -p 9256:8000 \
  -e TOP_N=100 \
  -e CACHE_SECONDS=10 \
  --pid=host \
  --privileged \
  top-process-exporter
```

### Using Python Directly

```bash
# Install dependencies
pip install -r requirements.txt

# Run the exporter
python exporter.py
```

## Configuration

Configuration is done via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| TOP_N | Number of top processes to expose | 100 |
| CACHE_SECONDS | Time to cache results (in seconds) | 10 |
| PORT | Port to expose the HTTP server on | 8000 |

## Endpoints

- `/metrics` - Prometheus metrics endpoint
- `/health` - Health check endpoint
- `/` - Basic information page

## Prometheus Configuration

Add the following to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'top-process-exporter'
    static_configs:
      - targets: ['localhost:9256']
    scrape_interval: 15s
```

## Example Metrics

```
# HELP top_process_cpu CPU usage percentage by process
# TYPE top_process_cpu gauge
top_process_cpu{name="chrome", pid="1234", user="john"} 25.5
# HELP top_process_memory Memory usage percentage by process
# TYPE top_process_memory gauge
top_process_memory{name="chrome", pid="1234", user="john"} 15.2
# HELP top_process_memory_bytes Memory usage in bytes by process
# TYPE top_process_memory_bytes gauge
top_process_memory_bytes{name="chrome", pid="1234", user="john"} 1234567890
```

## Security Considerations

This exporter requires privileged access to gather process information from the host. In production environments, consider:

1. Running the container with the minimum required privileges
2. Implementing proper network security to restrict access to the exporter
3. Reviewing the access requirements regularly

## Grafana Dashboard

A sample Grafana dashboard is available in the `dashboards` directory.

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request. 