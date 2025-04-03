package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/v3/process"
)

// Configuration options
type Config struct {
	Port        int
	TopN        int
	CacheTime   int
	MetricsPath string
}

// Process metrics collector
type ProcessCollector struct {
	mutex       sync.RWMutex
	topN        int
	lastCollect time.Time
	cacheTime   int
	cpuDesc     *prometheus.Desc
	memPercDesc *prometheus.Desc
	memBytesDesc *prometheus.Desc
	threadDesc  *prometheus.Desc
	uptimeDesc  *prometheus.Desc
	cpuSystemDesc *prometheus.Desc
	cpuUserDesc *prometheus.Desc
	processes   []ProcessInfo
}

// ProcessInfo stores information about a single process
type ProcessInfo struct {
	PID          int32
	Name         string
	Username     string
	CpuPercent   float64
	MemPercent   float32
	MemBytes     uint64
	ThreadCount  int32
	CreateTime   int64
	CpuUserTime  float64
	CpuSystemTime float64
}

// Create a new ProcessCollector
func NewProcessCollector(topN, cacheTime int) *ProcessCollector {
	return &ProcessCollector{
		topN:      topN,
		cacheTime: cacheTime,
		cpuDesc: prometheus.NewDesc(
			"top_process_cpu",
			"CPU usage percentage by process",
			[]string{"pid", "name", "user"}, nil,
		),
		memPercDesc: prometheus.NewDesc(
			"top_process_memory",
			"Memory usage percentage by process",
			[]string{"pid", "name", "user"}, nil,
		),
		memBytesDesc: prometheus.NewDesc(
			"top_process_memory_bytes",
			"Memory usage in bytes by process",
			[]string{"pid", "name", "user"}, nil,
		),
		threadDesc: prometheus.NewDesc(
			"top_process_threads",
			"Number of threads",
			[]string{"pid", "name", "user"}, nil,
		),
		uptimeDesc: prometheus.NewDesc(
			"top_process_uptime",
			"Process uptime in seconds",
			[]string{"pid", "name", "user"}, nil,
		),
		cpuSystemDesc: prometheus.NewDesc(
			"top_process_cpu_system",
			"System CPU time by process",
			[]string{"pid", "name", "user"}, nil,
		),
		cpuUserDesc: prometheus.NewDesc(
			"top_process_cpu_user",
			"User CPU time by process",
			[]string{"pid", "name", "user"}, nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *ProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuDesc
	ch <- c.memPercDesc
	ch <- c.memBytesDesc
	ch <- c.threadDesc
	ch <- c.uptimeDesc
	ch <- c.cpuSystemDesc
	ch <- c.cpuUserDesc
}

// Collect implements prometheus.Collector.
func (c *ProcessCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.RLock()
	// Check if cache is still valid
	if !c.lastCollect.IsZero() && time.Since(c.lastCollect).Seconds() < float64(c.cacheTime) {
		processes := c.processes
		c.mutex.RUnlock()
		// Use cached data
		for _, p := range processes {
			pid := strconv.Itoa(int(p.PID))
			now := time.Now().Unix()
			uptime := float64(now - p.CreateTime)
			
			ch <- prometheus.MustNewConstMetric(c.cpuDesc, prometheus.GaugeValue, p.CpuPercent, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.memPercDesc, prometheus.GaugeValue, float64(p.MemPercent), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.memBytesDesc, prometheus.GaugeValue, float64(p.MemBytes), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.threadDesc, prometheus.GaugeValue, float64(p.ThreadCount), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.uptimeDesc, prometheus.GaugeValue, uptime, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.cpuSystemDesc, prometheus.CounterValue, p.CpuSystemTime, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.cpuUserDesc, prometheus.CounterValue, p.CpuUserTime, pid, p.Name, p.Username)
		}
		return
	}
	c.mutex.RUnlock()

	// We need to refresh data
	c.mutex.Lock()
	defer c.mutex.Unlock()
	// Double-check if cache is still invalid (in case another goroutine updated it while we were waiting for the lock)
	if !c.lastCollect.IsZero() && time.Since(c.lastCollect).Seconds() < float64(c.cacheTime) {
		processes := c.processes
		// Use cached data
		for _, p := range processes {
			pid := strconv.Itoa(int(p.PID))
			now := time.Now().Unix()
			uptime := float64(now - p.CreateTime)
			
			ch <- prometheus.MustNewConstMetric(c.cpuDesc, prometheus.GaugeValue, p.CpuPercent, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.memPercDesc, prometheus.GaugeValue, float64(p.MemPercent), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.memBytesDesc, prometheus.GaugeValue, float64(p.MemBytes), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.threadDesc, prometheus.GaugeValue, float64(p.ThreadCount), pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.uptimeDesc, prometheus.GaugeValue, uptime, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.cpuSystemDesc, prometheus.CounterValue, p.CpuSystemTime, pid, p.Name, p.Username)
			ch <- prometheus.MustNewConstMetric(c.cpuUserDesc, prometheus.CounterValue, p.CpuUserTime, pid, p.Name, p.Username)
		}
		return
	}

	log.Println("Collecting process metrics...")
	// Get all processes
	processes, err := process.Processes()
	if err != nil {
		log.Printf("Error fetching processes: %v", err)
		return
	}

	// First call to initialize CPU percentage
	for _, p := range processes {
		_, _ = p.CPUPercent()
	}

	// Short delay for CPU metrics to be accurate
	time.Sleep(100 * time.Millisecond)

	// Collect detailed data
	var processInfoList []ProcessInfo
	for _, p := range processes {
		info, err := getProcessInfo(p)
		if err != nil {
			continue
		}
		processInfoList = append(processInfoList, info)
	}

	// Store top N processes by CPU and memory
	c.processes = getTopProcesses(processInfoList, c.topN)
	c.lastCollect = time.Now()

	// Emit metrics
	for _, p := range c.processes {
		pid := strconv.Itoa(int(p.PID))
		now := time.Now().Unix()
		uptime := float64(now - p.CreateTime)
		
		ch <- prometheus.MustNewConstMetric(c.cpuDesc, prometheus.GaugeValue, p.CpuPercent, pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.memPercDesc, prometheus.GaugeValue, float64(p.MemPercent), pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.memBytesDesc, prometheus.GaugeValue, float64(p.MemBytes), pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.threadDesc, prometheus.GaugeValue, float64(p.ThreadCount), pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.uptimeDesc, prometheus.GaugeValue, uptime, pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.cpuSystemDesc, prometheus.CounterValue, p.CpuSystemTime, pid, p.Name, p.Username)
		ch <- prometheus.MustNewConstMetric(c.cpuUserDesc, prometheus.CounterValue, p.CpuUserTime, pid, p.Name, p.Username)
	}
}

// Get information about a process
func getProcessInfo(p *process.Process) (ProcessInfo, error) {
	info := ProcessInfo{PID: p.Pid}

	// Name
	name, err := p.Name()
	if err != nil {
		return info, err
	}
	info.Name = name

	// Username
	username, err := p.Username()
	if err != nil {
		username = "unknown"
	}
	info.Username = username

	// CPU
	cpuPercent, err := p.CPUPercent()
	if err != nil {
		return info, err
	}
	info.CpuPercent = cpuPercent

	// Memory
	memInfo, err := p.MemoryInfo()
	if err != nil {
		return info, err
	}
	if memInfo != nil {
		info.MemBytes = memInfo.RSS
	}

	memPercent, err := p.MemoryPercent()
	if err != nil {
		return info, err
	}
	info.MemPercent = memPercent

	// Threads
	numThreads, err := p.NumThreads()
	if err == nil {
		info.ThreadCount = numThreads
	}

	// Creation time
	createTime, err := p.CreateTime()
	if err == nil {
		info.CreateTime = createTime / 1000 // Convert to seconds
	}

	// CPU times
	times, err := p.Times()
	if err == nil {
		info.CpuUserTime = times.User
		info.CpuSystemTime = times.System
	}

	return info, nil
}

// Get top N processes by CPU and Memory
func getTopProcesses(processes []ProcessInfo, n int) []ProcessInfo {
	if len(processes) == 0 {
		return []ProcessInfo{}
	}

	// Sort by CPU (descending)
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CpuPercent > processes[j].CpuPercent
	})

	// Get top N by CPU
	topCPU := make([]ProcessInfo, 0, n)
	for i := 0; i < n && i < len(processes); i++ {
		topCPU = append(topCPU, processes[i])
	}

	// Sort by Memory (descending)
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].MemPercent > processes[j].MemPercent
	})

	// Get top N by Memory
	topMemory := make([]ProcessInfo, 0, n)
	for i := 0; i < n && i < len(processes); i++ {
		// Check if process is already in topCPU
		found := false
		for _, p := range topCPU {
			if p.PID == processes[i].PID {
				found = true
				break
			}
		}
		if !found {
			topMemory = append(topMemory, processes[i])
		}
	}

	// Combine lists
	return append(topCPU, topMemory...)
}

// Health check handler
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy","timestamp":%d}`, time.Now().Unix())
}

// Root handler
func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Top Process Exporter</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
        }
        h1 {
            color: #333;
        }
        li {
            margin-bottom: 8px;
        }
    </style>
</head>
<body>
    <h1>Top N Process Exporter</h1>
    <p>A Prometheus exporter for monitoring top CPU and memory processes.</p>
    <p>Metrics available at <a href='/metrics'>/metrics</a></p>
    <p>Configuration:</p>
    <ul>
        <li>TOP_N: %d</li>
        <li>CACHE_SECONDS: %d</li>
    </ul>
</body>
</html>`
	fmt.Fprintf(w, html, config.TopN, config.CacheTime)
}

// Get environment variable or default
func getEnvOrDefault(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// Global configuration
var config Config

func main() {
	// Parse command line flags
	flag.IntVar(&config.Port, "port", 0, "Port to listen on")
	flag.IntVar(&config.TopN, "topn", 0, "Number of top processes to report")
	flag.IntVar(&config.CacheTime, "cache", 0, "Seconds to cache results")
	flag.StringVar(&config.MetricsPath, "metrics-path", "", "Path to expose metrics on")
	flag.Parse()

	// Read environment variables (with precedence over command-line flags)
	if portStr := getEnvOrDefault("PORT", "8000"); config.Port == 0 {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		} else {
			config.Port = 8000
		}
	}

	if topNStr := getEnvOrDefault("TOP_N", "100"); config.TopN == 0 {
		if topN, err := strconv.Atoi(topNStr); err == nil {
			config.TopN = topN
		} else {
			config.TopN = 100
		}
	}

	if cacheStr := getEnvOrDefault("CACHE_SECONDS", "10"); config.CacheTime == 0 {
		if cache, err := strconv.Atoi(cacheStr); err == nil {
			config.CacheTime = cache
		} else {
			config.CacheTime = 10
		}
	}

	if config.MetricsPath == "" {
		config.MetricsPath = getEnvOrDefault("METRICS_PATH", "/metrics")
	}

	// Create collector
	collector := NewProcessCollector(config.TopN, config.CacheTime)
	
	// Register collector
	prometheus.MustRegister(collector)

	// Register handlers
	http.Handle(config.MetricsPath, promhttp.Handler())
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", rootHandler)

	// Start server
	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Starting Top Process Exporter (TOP_N=%d, CACHE_SECONDS=%d)", config.TopN, config.CacheTime)
	log.Printf("Metrics available at http://0.0.0.0:%d%s", config.Port, config.MetricsPath)
	log.Fatal(http.ListenAndServe(addr, nil))
} 