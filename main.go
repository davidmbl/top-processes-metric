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
	MaxConcurrent int
}

// Process metrics collector
type ProcessCollector struct {
	mutex         sync.RWMutex
	topN          int
	lastCollect   time.Time
	cacheTime     int
	cpuDesc       *prometheus.Desc
	memPercDesc   *prometheus.Desc
	memBytesDesc  *prometheus.Desc
	threadDesc    *prometheus.Desc
	uptimeDesc    *prometheus.Desc
	cpuSystemDesc *prometheus.Desc
	cpuUserDesc   *prometheus.Desc
	processes     []ProcessInfo
	collecting    bool
	collectMutex  sync.Mutex
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
		collecting: false,
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
			c.emitProcessMetrics(ch, p)
		}
		return
	}
	c.mutex.RUnlock()

	// Try to acquire the collection lock - if another goroutine is already collecting, use existing data
	if !c.collectMutex.TryLock() {
		c.mutex.RLock()
		processes := c.processes
		c.mutex.RUnlock()
		log.Println("Another collection in progress, using cached data")
		for _, p := range processes {
			c.emitProcessMetrics(ch, p)
		}
		return
	}
	defer c.collectMutex.Unlock()

	// We need to refresh data
	c.mutex.Lock()
	// Double-check if cache is still invalid (in case another goroutine updated it while we were waiting for the lock)
	if !c.lastCollect.IsZero() && time.Since(c.lastCollect).Seconds() < float64(c.cacheTime) {
		processes := c.processes
		c.mutex.Unlock()
		// Use cached data
		for _, p := range processes {
			c.emitProcessMetrics(ch, p)
		}
		return
	}
	c.mutex.Unlock()

	log.Println("Collecting process metrics...")
	
	// Get all processes
	processes, err := process.Processes()
	if err != nil {
		log.Printf("Error fetching processes: %v", err)
		return
	}

	// First pass: quick filter by CPU to reduce workload
	// This gives a rough estimate to filter out processes not worth examining in detail
	prelimProcesses := make([]*process.Process, 0, len(processes))
	for _, p := range processes {
		cpuPercent, err := p.CPUPercent()
		if err == nil && cpuPercent > 0.1 { // Only consider processes with non-negligible CPU
			prelimProcesses = append(prelimProcesses, p)
		}
	}

	// Sort by preliminary CPU usage to focus on likely top processes
	sort.Slice(prelimProcesses, func(i, j int) bool {
		cpu1, _ := prelimProcesses[i].CPUPercent()
		cpu2, _ := prelimProcesses[j].CPUPercent()
		return cpu1 > cpu2
	})

	// Take a reasonable number to process in detail (2x topN to ensure we don't miss any)
	candidateCount := c.topN * 2
	if len(prelimProcesses) > candidateCount {
		prelimProcesses = prelimProcesses[:candidateCount]
	}

	// Collect detailed data for filtered processes in parallel
	processChan := make(chan ProcessInfo, len(prelimProcesses))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Reduced from 10 to 5 to limit concurrency

	for _, p := range prelimProcesses {
		wg.Add(1)
		semaphore <- struct{}{} // Acquire semaphore
		go func(proc *process.Process) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release semaphore
			
			info, err := getProcessInfo(proc)
			if err != nil {
				return
			}
			processChan <- info
		}(p)
	}

	// Close channel when all goroutines are done
	go func() {
		wg.Wait()
		close(processChan)
	}()

	// Collect results
	var processInfoList []ProcessInfo
	for info := range processChan {
		processInfoList = append(processInfoList, info)
	}

	// Store top N processes by CPU and memory
	topProcesses := getTopProcesses(processInfoList, c.topN)
	
	c.mutex.Lock()
	c.processes = topProcesses
	c.lastCollect = time.Now()
	c.mutex.Unlock()

	// Emit metrics
	for _, p := range topProcesses {
		c.emitProcessMetrics(ch, p)
	}
}

// Helper method to emit metrics for a process
func (c *ProcessCollector) emitProcessMetrics(ch chan<- prometheus.Metric, p ProcessInfo) {
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

	// Create time
	createTime, err := p.CreateTime()
	if err == nil {
		info.CreateTime = createTime / 1000 // Convert to seconds
	}

	// CPU times
	cpuTimes, err := p.Times()
	if err == nil {
		info.CpuUserTime = cpuTimes.User
		info.CpuSystemTime = cpuTimes.System
	}

	return info, nil
}

// Get top N processes by CPU and memory
func getTopProcesses(processes []ProcessInfo, topN int) []ProcessInfo {
	// Sort by CPU percentage
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CpuPercent > processes[j].CpuPercent
	})

	// Take top N processes
	if len(processes) > topN {
		processes = processes[:topN]
	}

	return processes
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
        <li>MAX_CONCURRENT: %d</li>
    </ul>
</body>
</html>`
	fmt.Fprintf(w, html, config.TopN, config.CacheTime, config.MaxConcurrent)
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
	flag.IntVar(&config.MaxConcurrent, "max-concurrent", 0, "Maximum concurrent metrics scrapes")
	flag.Parse()

	// Read environment variables (with precedence over command-line flags)
	if portStr := getEnvOrDefault("PORT", "8000"); config.Port == 0 {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		} else {
			config.Port = 8000
		}
	}

	if topNStr := getEnvOrDefault("TOP_N", "30"); config.TopN == 0 {
		if topN, err := strconv.Atoi(topNStr); err == nil {
			config.TopN = topN
		} else {
			config.TopN = 30
		}
	}

	if cacheStr := getEnvOrDefault("CACHE_SECONDS", "20"); config.CacheTime == 0 {
		if cache, err := strconv.Atoi(cacheStr); err == nil {
			config.CacheTime = cache
		} else {
			config.CacheTime = 20
		}
	}

	if maxConcurrentStr := getEnvOrDefault("MAX_CONCURRENT", "2"); config.MaxConcurrent == 0 {
		if maxConcurrent, err := strconv.Atoi(maxConcurrentStr); err == nil {
			config.MaxConcurrent = maxConcurrent
		} else {
			config.MaxConcurrent = 2
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
	http.Handle(config.MetricsPath, promhttp.InstrumentMetricHandler(
		prometheus.DefaultRegisterer, promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				MaxRequestsInFlight: config.MaxConcurrent,
				Timeout:            60 * time.Second,
			},
		),
	))
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", rootHandler)

	// Start server
	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Starting Top Process Exporter (TOP_N=%d, CACHE_SECONDS=%d, MAX_CONCURRENT=%d)", 
		config.TopN, config.CacheTime, config.MaxConcurrent)
	log.Printf("Metrics available at http://0.0.0.0:%d%s", config.Port, config.MetricsPath)
	log.Fatal(http.ListenAndServe(addr, nil))
} 