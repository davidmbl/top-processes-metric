#!/usr/bin/env python3
# Top Services Process Exporter
# A standalone Prometheus exporter for monitoring top processes by CPU and memory

import os
import time
import logging
from flask import Flask, Response, jsonify
import psutil

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
)
logger = logging.getLogger('top-process-exporter')

app = Flask(__name__)

# Configuration via environment variables
TOP_N = int(os.getenv("TOP_N", "100"))
CACHE_SECONDS = int(os.getenv("CACHE_SECONDS", "10"))
PORT = int(os.getenv("PORT", "8000"))

# Cache for metrics to reduce system impact on frequent scrapes
metrics_cache = {
    'last_update': 0,
    'data': ''
}

def collect_process_metrics():
    """Collect top N process metrics sorted by CPU and memory usage"""
    try:
        processes = []
        # Update CPU usage for all processes
        for _ in range(2):  # First call to initialize, second to get actual values
            for p in psutil.process_iter(['pid', 'name', 'username', 'create_time']):
                try:
                    p.cpu_percent()
                except (psutil.NoSuchProcess, psutil.AccessDenied):
                    pass
            if _ == 0:  # Sleep briefly after first iteration to get accurate CPU percentage
                time.sleep(0.1)
        
        # Collect detailed process info
        for p in psutil.process_iter():
            try:
                with p.oneshot():  # Get all process info in a single call
                    info = {
                        'pid': p.pid,
                        'name': p.name(),
                        'username': p.username(),
                        'cpu_percent': p.cpu_percent(),
                        'memory_percent': p.memory_percent(),
                        'memory_bytes': p.memory_info().rss,
                        'create_time': p.create_time(),
                        'uptime': time.time() - p.create_time(),
                        'num_threads': p.num_threads(),
                        'cpu_times': p.cpu_times()
                    }
                    processes.append(info)
            except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess) as e:
                logger.debug(f"Error collecting process {p.pid}: {str(e)}")
                continue
            
        return processes
    except Exception as e:
        logger.error(f"Error collecting process metrics: {str(e)}")
        return []

def format_prometheus_metrics(processes):
    """Format process data as Prometheus metrics"""
    if not processes:
        return "# No process data collected"
    
    # Sort by CPU and memory
    top_cpu = sorted(processes, key=lambda p: p['cpu_percent'], reverse=True)[:TOP_N]
    top_mem = sorted(processes, key=lambda p: p['memory_percent'], reverse=True)[:TOP_N]
    
    lines = [
        "# HELP top_process_cpu CPU usage percentage by process",
        "# TYPE top_process_cpu gauge",
        "# HELP top_process_memory Memory usage percentage by process",
        "# TYPE top_process_memory gauge",
        "# HELP top_process_memory_bytes Memory usage in bytes by process",
        "# TYPE top_process_memory_bytes gauge",
        "# HELP top_process_uptime Process uptime in seconds",
        "# TYPE top_process_uptime gauge",
        "# HELP top_process_threads Number of threads",
        "# TYPE top_process_threads gauge",
        "# HELP top_process_cpu_system System CPU time by process",
        "# TYPE top_process_cpu_system counter",
        "# HELP top_process_cpu_user User CPU time by process",
        "# TYPE top_process_cpu_user counter",
    ]
    
    # Add CPU metrics
    for p in top_cpu:
        name = p['name'].replace('"', '').replace('\\', '\\\\')
        username = p['username'].replace('"', '').replace('\\', '\\\\')
        lines.append(f'top_process_cpu{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["cpu_percent"]}')
        # Include additional metrics
        lines.append(f'top_process_uptime{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["uptime"]}')
        lines.append(f'top_process_threads{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["num_threads"]}')
        lines.append(f'top_process_cpu_system{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["cpu_times"].system}')
        lines.append(f'top_process_cpu_user{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["cpu_times"].user}')
    
    # Add memory metrics
    for p in top_mem:
        name = p['name'].replace('"', '').replace('\\', '\\\\')
        username = p['username'].replace('"', '').replace('\\', '\\\\')
        lines.append(f'top_process_memory{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["memory_percent"]}')
        lines.append(f'top_process_memory_bytes{{name="{name}", pid="{p["pid"]}", user="{username}"}} {p["memory_bytes"]}')
    
    # Add exporter self-monitoring
    lines.append(f'top_process_exporter_top_n {TOP_N}')
    lines.append(f'top_process_exporter_cache_seconds {CACHE_SECONDS}')
    lines.append(f'top_process_exporter_process_count {len(processes)}')
    
    return "\n".join(lines)

@app.route("/metrics")
def metrics():
    """Prometheus metrics endpoint"""
    current_time = time.time()
    
    # Use cached data if it's still fresh
    if current_time - metrics_cache['last_update'] < CACHE_SECONDS:
        logger.debug("Serving metrics from cache")
        return Response(metrics_cache['data'], mimetype="text/plain")
    
    # Collect new metrics
    logger.info("Collecting new process metrics")
    processes = collect_process_metrics()
    metrics_text = format_prometheus_metrics(processes)
    
    # Update cache
    metrics_cache['last_update'] = current_time
    metrics_cache['data'] = metrics_text
    
    return Response(metrics_text, mimetype="text/plain")

@app.route("/")
def root():
    """Root endpoint with basic information"""
    return """
    <h1>Top N Process Exporter</h1>
    <p>A Prometheus exporter for monitoring top CPU and memory processes.</p>
    <p>Metrics available at <a href='/metrics'>/metrics</a></p>
    <p>Configuration:</p>
    <ul>
        <li>TOP_N: {top_n}</li>
        <li>CACHE_SECONDS: {cache_seconds}</li>
    </ul>
    """.format(top_n=TOP_N, cache_seconds=CACHE_SECONDS)

@app.route("/health")
def health():
    """Health check endpoint for Docker and monitoring"""
    return jsonify({"status": "healthy", "timestamp": time.time()})

if __name__ == "__main__":
    logger.info(f"Starting Top Process Exporter (TOP_N={TOP_N}, CACHE_SECONDS={CACHE_SECONDS})")
    logger.info(f"Metrics available at http://0.0.0.0:{PORT}/metrics")
    app.run(host="0.0.0.0", port=PORT) 