package middleware

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// MonitorEventFunc is a callback invoked when the system monitor emits a notification.
// eventType is a short string such as "debug", "warning"; content is the message body.
// This intentionally avoids importing pkg/event to prevent import cycles.
type MonitorEventFunc func(eventType, content string)

// SystemMetrics holds a point-in-time snapshot of Go runtime metrics.
type SystemMetrics struct {
	CPUUsage    float64       `json:"cpu_usage"`
	MemoryUsage uint64        `json:"memory_usage"`
	MemoryTotal uint64        `json:"memory_total"`
	Goroutines  int           `json:"goroutines"`
	GCPauses    []float64     `json:"gc_pauses"`
	LastGCTime  time.Time     `json:"last_gc_time"`
	Uptime      time.Duration `json:"uptime"`
	Timestamp   time.Time     `json:"timestamp"`
}

// PerformanceMetrics holds aggregated request/tool performance data.
type PerformanceMetrics struct {
	APIRequestCount       int64         `json:"api_request_count"`
	APIRequestDuration    time.Duration `json:"api_request_duration"`
	ToolExecutionCount    int64         `json:"tool_execution_count"`
	ToolExecutionDuration time.Duration `json:"tool_execution_duration"`
	ErrorRate             float64       `json:"error_rate"`
	Throughput            float64       `json:"throughput"`
	Latency               time.Duration `json:"latency"`
	Timestamp             time.Time     `json:"timestamp"`
}

// HealthStatus summarises overall system health.
type HealthStatus struct {
	Overall    string                 `json:"overall"`
	Components map[string]string      `json:"components"`
	Issues     []string               `json:"issues"`
	Metrics    map[string]interface{} `json:"metrics"`
	Timestamp  time.Time              `json:"timestamp"`
}

// SystemMonitor collects Go runtime and performance metrics and calls the
// provided MonitorEventFunc when thresholds are exceeded.
// It is safe for concurrent use.
type SystemMonitor struct {
	mu                 sync.RWMutex
	startTime          time.Time
	eventFn            MonitorEventFunc
	metricsHistory     []SystemMetrics
	perfHistory        []PerformanceMetrics
	maxHistorySize     int
	monitoringActive   bool
	monitoringCtx      context.Context //nolint:containedctx
	monitoringCancel   context.CancelFunc
	monitoringInterval time.Duration

	// performance counters
	apiRequestCount int64
	apiRequestTotal time.Duration
	toolExecCount   int64
	toolExecTotal   time.Duration
	errorCount      int64
	totalRequests   int64

	// component health
	componentHealth map[string]string
	healthCheckers  map[string]func() (string, error)
}

// NewSystemMonitor creates a new SystemMonitor.
// eventFn may be nil to disable event emission.
func NewSystemMonitor(eventFn MonitorEventFunc) *SystemMonitor {
	return &SystemMonitor{
		startTime:          time.Now(),
		eventFn:            eventFn,
		metricsHistory:     make([]SystemMetrics, 0),
		perfHistory:        make([]PerformanceMetrics, 0),
		maxHistorySize:     100,
		monitoringInterval: 30 * time.Second,
		componentHealth:    make(map[string]string),
		healthCheckers:     make(map[string]func() (string, error)),
	}
}

// Start begins background metric collection.
func (sm *SystemMonitor) Start(ctx context.Context) {
	sm.mu.Lock()
	if sm.monitoringActive {
		sm.mu.Unlock()
		return
	}
	sm.monitoringCtx, sm.monitoringCancel = context.WithCancel(ctx)
	sm.monitoringActive = true
	fn := sm.eventFn
	sm.mu.Unlock()

	go sm.monitoringLoop()

	if fn != nil {
		fn("debug", "system monitor started")
	}
	logger.Info("system monitor started")
}

// Stop stops background metric collection.
func (sm *SystemMonitor) Stop() {
	sm.mu.Lock()
	if !sm.monitoringActive {
		sm.mu.Unlock()
		return
	}
	sm.monitoringCancel()
	sm.monitoringActive = false
	fn := sm.eventFn
	sm.mu.Unlock()

	if fn != nil {
		fn("debug", "system monitor stopped")
	}
	logger.Info("system monitor stopped")
}

func (sm *SystemMonitor) monitoringLoop() {
	ticker := time.NewTicker(sm.monitoringInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sm.monitoringCtx.Done():
			return
		case <-ticker.C:
			sm.collectMetrics()
			sm.checkHealth()
		}
	}
}

func (sm *SystemMonitor) collectMetrics() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	sys := SystemMetrics{
		MemoryUsage: memStats.Alloc,
		MemoryTotal: memStats.Sys,
		Goroutines:  runtime.NumGoroutine(),
		LastGCTime:  time.Unix(0, int64(memStats.LastGC)),
		Uptime:      time.Since(sm.startTime),
		Timestamp:   time.Now(),
	}
	if len(memStats.PauseNs) > 0 {
		sys.GCPauses = make([]float64, len(memStats.PauseNs))
		for i, p := range memStats.PauseNs {
			sys.GCPauses[i] = float64(p) / 1e6
		}
	}

	perf := sm.calcPerformanceMetrics()
	sm.addToHistory(sys, perf)
	sm.checkMetricThresholds(sys, perf)
}

func (sm *SystemMonitor) calcPerformanceMetrics() PerformanceMetrics {
	var avgAPI, avgTool time.Duration
	var errRate, throughput float64

	if sm.apiRequestCount > 0 {
		avgAPI = sm.apiRequestTotal / time.Duration(sm.apiRequestCount)
	}
	if sm.toolExecCount > 0 {
		avgTool = sm.toolExecTotal / time.Duration(sm.toolExecCount)
	}
	if sm.totalRequests > 0 {
		errRate = float64(sm.errorCount) / float64(sm.totalRequests)
		throughput = float64(sm.totalRequests) / time.Since(sm.startTime).Seconds()
	}

	return PerformanceMetrics{
		APIRequestCount:       sm.apiRequestCount,
		APIRequestDuration:    avgAPI,
		ToolExecutionCount:    sm.toolExecCount,
		ToolExecutionDuration: avgTool,
		ErrorRate:             errRate,
		Throughput:            throughput,
		Latency:               avgAPI,
		Timestamp:             time.Now(),
	}
}

func (sm *SystemMonitor) addToHistory(sys SystemMetrics, perf PerformanceMetrics) {
	if len(sm.metricsHistory) >= sm.maxHistorySize {
		sm.metricsHistory = sm.metricsHistory[1:]
	}
	sm.metricsHistory = append(sm.metricsHistory, sys)

	if len(sm.perfHistory) >= sm.maxHistorySize {
		sm.perfHistory = sm.perfHistory[1:]
	}
	sm.perfHistory = append(sm.perfHistory, perf)
}

func (sm *SystemMonitor) checkMetricThresholds(sys SystemMetrics, perf PerformanceMetrics) {
	if sm.eventFn == nil {
		return
	}
	if sys.MemoryUsage > 1024*1024*1024 {
		sm.eventFn("warning", fmt.Sprintf("high memory: %.2f MB", float64(sys.MemoryUsage)/1024/1024))
	}
	if sys.Goroutines > 1000 {
		sm.eventFn("warning", fmt.Sprintf("goroutine spike: %d", sys.Goroutines))
	}
	if perf.ErrorRate > 0.1 {
		sm.eventFn("warning", fmt.Sprintf("high error rate: %.2f%%", perf.ErrorRate*100))
	}
	if perf.Latency > 10*time.Second {
		sm.eventFn("warning", fmt.Sprintf("high latency: %v", perf.Latency))
	}
}

func (sm *SystemMonitor) checkHealth() {
	sm.mu.Lock()
	checkers := make(map[string]func() (string, error), len(sm.healthCheckers))
	for k, v := range sm.healthCheckers {
		checkers[k] = v
	}
	fn := sm.eventFn
	sm.mu.Unlock()

	health := HealthStatus{
		Overall:    "healthy",
		Components: make(map[string]string),
		Issues:     []string{},
		Metrics:    make(map[string]interface{}),
		Timestamp:  time.Now(),
	}
	for component, checker := range checkers {
		status, err := checker()
		if err != nil {
			status = "unhealthy"
			health.Issues = append(health.Issues, fmt.Sprintf("%s: %v", component, err))
		}
		health.Components[component] = status
	}
	for _, status := range health.Components {
		if status == "unhealthy" {
			health.Overall = "unhealthy"
			break
		} else if status == "warning" && health.Overall == "healthy" {
			health.Overall = "warning"
		}
	}

	sm.mu.Lock()
	for k, v := range health.Components {
		sm.componentHealth[k] = v
	}
	if len(sm.metricsHistory) > 0 {
		l := sm.metricsHistory[len(sm.metricsHistory)-1]
		health.Metrics["memory_usage_mb"] = float64(l.MemoryUsage) / 1024 / 1024
		health.Metrics["goroutines"] = l.Goroutines
		health.Metrics["uptime_seconds"] = l.Uptime.Seconds()
	}
	if len(sm.perfHistory) > 0 {
		l := sm.perfHistory[len(sm.perfHistory)-1]
		health.Metrics["error_rate"] = l.ErrorRate
		health.Metrics["throughput"] = l.Throughput
		health.Metrics["latency_ms"] = l.Latency.Milliseconds()
	}
	sm.mu.Unlock()

	if fn != nil && health.Overall != "healthy" {
		fn("warning", fmt.Sprintf("system health: %s", health.Overall))
	}
}

// RecordAPIRequest records an API call duration and success flag.
func (sm *SystemMonitor) RecordAPIRequest(duration time.Duration, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.apiRequestCount++
	sm.apiRequestTotal += duration
	sm.totalRequests++
	if !success {
		sm.errorCount++
	}
}

// RecordToolExecution records a tool call duration and success flag.
func (sm *SystemMonitor) RecordToolExecution(duration time.Duration, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.toolExecCount++
	sm.toolExecTotal += duration
	sm.totalRequests++
	if !success {
		sm.errorCount++
	}
}

// RegisterHealthChecker registers a named health-check function.
func (sm *SystemMonitor) RegisterHealthChecker(component string, checker func() (string, error)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.healthCheckers[component] = checker
	sm.componentHealth[component] = "unknown"
}

// GetCurrentMetrics returns the most recent system and performance metrics snapshot.
func (sm *SystemMonitor) GetCurrentMetrics() (SystemMetrics, PerformanceMetrics) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var sys SystemMetrics
	var perf PerformanceMetrics
	if len(sm.metricsHistory) > 0 {
		sys = sm.metricsHistory[len(sm.metricsHistory)-1]
	}
	if len(sm.perfHistory) > 0 {
		perf = sm.perfHistory[len(sm.perfHistory)-1]
	}
	return sys, perf
}

// GetHealthStatus returns the current aggregated health status.
func (sm *SystemMonitor) GetHealthStatus() HealthStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	h := HealthStatus{
		Overall:    "healthy",
		Components: make(map[string]string),
		Issues:     []string{},
		Metrics:    make(map[string]interface{}),
		Timestamp:  time.Now(),
	}
	for k, v := range sm.componentHealth {
		h.Components[k] = v
		if v == "unhealthy" {
			h.Overall = "unhealthy"
		} else if v == "warning" && h.Overall == "healthy" {
			h.Overall = "warning"
		}
	}
	return h
}

// GetMetricsHistory returns up to limit recent history entries.
func (sm *SystemMonitor) GetMetricsHistory(limit int) ([]SystemMetrics, []PerformanceMetrics) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sys := sm.metricsHistory
	perf := sm.perfHistory
	if limit > 0 && limit < len(sys) {
		sys = sys[len(sys)-limit:]
	}
	if limit > 0 && limit < len(perf) {
		perf = perf[len(perf)-limit:]
	}
	return sys, perf
}

// PrintDiagnostics logs the current system and performance metrics summary.
func (sm *SystemMonitor) PrintDiagnostics() {
	sys, perf := sm.GetCurrentMetrics()
	health := sm.GetHealthStatus()

	fmt.Println("=== System Diagnostics ===")
	logger.Infof("health:       %s", health.Overall)
	logger.Infof("uptime:       %v", sys.Uptime)
	logger.Infof("memory:       %.2f MB", float64(sys.MemoryUsage)/1024/1024)
	logger.Infof("goroutines:   %d", sys.Goroutines)
	logger.Infof("api calls:    %d", perf.APIRequestCount)
	logger.Infof("tool calls:   %d", perf.ToolExecutionCount)
	logger.Infof("error rate:   %.2f%%", perf.ErrorRate*100)
	logger.Infof("throughput:   %.2f req/s", perf.Throughput)
	logger.Infof("avg latency:  %v", perf.Latency)
	fmt.Println("==========================")
}
