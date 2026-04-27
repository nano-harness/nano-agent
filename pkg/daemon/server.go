package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/version"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const teamLeadStreamReadTimeout = 300 * time.Second

// Server represents the daemon mode server
type Server struct {
	agent         *agent.Agent
	httpServer    *http.Server
	pprofServer   *http.Server
	config        *config.DaemonConfig
	upgrader      websocket.Upgrader
	systemMonitor *middleware.SystemMonitor
	scheduler     *cron.Scheduler
	startTime     time.Time

	// Task management for cancellation support
	activeTasks map[string]*ActiveTask
	tasksMutex  sync.RWMutex
	draining    bool

	// engineManaged indicates that scheduler lifecycle is managed by Engine
	engineManaged bool

	// teamLeadRegistry manages team-lead sessions (optional, nil if not using teams)
	teamLeadRegistry *TeamLeadRegistry
}

// ActiveTask represents a running task that can be cancelled
type ActiveTask struct {
	ID          string
	SessionID   string
	Command     string
	Images      []llm.MultimodalImage // Multimodal images attached to the command
	Title       string                // Generated title
	Type        string                // "interactive", "background", "unified"
	StartTime   time.Time
	EndTime     time.Time
	Cancel      context.CancelFunc
	Status      string            // "running", "completed", "cancelled", "error", "timeout", "incomplete"
	TokenUsage  *event.TokenStats // Store token usage statistics
	Scribe      *SessionScribe    // Event log recorder
	Broadcaster *EventBroadcaster // Handles fan-out of stream events
	Store       *TaskEventStore   // In-memory store for resumable streaming
	loadMutex   sync.Mutex        // Mutex for loading history
}

// EventBroadcaster handles fan-out of stream events
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan event.StreamEvent]struct{}
}

// NewEventBroadcaster creates a new event broadcaster
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		subscribers: make(map[chan event.StreamEvent]struct{}),
	}
}

// Subscribe subscribes to the event broadcaster
func (b *EventBroadcaster) Subscribe() chan event.StreamEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan event.StreamEvent, 100)
	b.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe unsubscribes from the event broadcaster
func (b *EventBroadcaster) Unsubscribe(ch chan event.StreamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
	close(ch)
}

// Publish publishes an event to all subscribers
func (b *EventBroadcaster) Publish(e event.StreamEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// Drop if full to prevent blocking
		}
	}
}

// Note: DaemonConfig is now consolidated in pkg/config package
// Import it from there instead of defining it here

// NewServer creates a new daemon server
func NewServer(agentInstance *agent.Agent, daemonConfig *config.DaemonConfig) *Server {
	if daemonConfig == nil {
		// Use default daemon config
		homeDir, _ := os.UserHomeDir()
		daemonConfig = &config.DaemonConfig{
			Port:       8080,
			Host:       "127.0.0.1",
			PidFile:    filepath.Join(homeDir, ".nano", "daemon.pid"),
			LogFile:    filepath.Join(homeDir, ".nano", "daemon.log"),
			EnableCORS: true,
			APIKey:     "",
		}
	}

	// Initialize system monitor
	systemMonitor := middleware.NewSystemMonitor(nil)

	server := &Server{
		agent:         agentInstance,
		config:        daemonConfig,
		systemMonitor: systemMonitor,
		activeTasks:   make(map[string]*ActiveTask),
		startTime:     time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return daemonConfig.EnableCORS // Allow CORS if enabled
			},
			EnableCompression: false, // Disable compression to avoid RSV bit issues
		},
	}

	server.scheduler = cron.New(func(command string) error {
		sessionID := fmt.Sprintf("scheduled-%d", time.Now().Unix())
		_, err := server.startUnifiedTask(command, sessionID, 0, nil)
		return err
	})
	server.EnableInteractiveApproval()

	// Initialize team-lead registry with 30 minute idle timeout
	// This can be disabled by setting NANO_DISABLE_TEAM_SESSIONS=true
	if os.Getenv("NANO_DISABLE_TEAM_SESSIONS") != "true" {
		server.teamLeadRegistry = NewTeamLeadRegistry(30 * time.Minute)
		logger.Info("Team-lead session registry initialized")
	}

	return server
}

func (ds *Server) EnableInteractiveApproval() {
	if ds == nil || ds.agent == nil {
		return
	}
	ds.agent.SetApprovalHandler(ds.requestToolApproval)
}

func (ds *Server) requestToolApproval(info *agent.ToolCallInfo) bool {
	if ds == nil || ds.agent == nil || info == nil {
		return false
	}
	if handler := ds.agent.GetEventHandler(); handler != nil {
		handler(event.StreamEvent{
			Type: event.EventTypeWaitingForUser,
			Metadata: map[string]interface{}{
				"kind":       "tool_approval_request",
				"call_id":    info.ID,
				"tool_name":  info.Name,
				"parameters": info.Parameters,
				"status":     string(info.Status),
			},
		})
	}
	return false
}

func (ds *Server) SubmitToolApproval(callID string, approved bool) error {
	if ds == nil || ds.agent == nil || ds.agent.GetToolScheduler() == nil {
		return fmt.Errorf("daemon approval handler is unavailable")
	}
	return ds.agent.GetToolScheduler().HandleConfirmationResponse(callID, approved)
}

// NewServerWithEngine builds a Server from a pre-constructed Engine.
// The Engine's Scheduler replaces the server's default instance so
// that all components share a single executor and state store.
func NewServerWithEngine(eng *engine.Engine, daemonConfig *config.DaemonConfig) *Server {
	server := NewServer(eng.Agent, daemonConfig)
	// Replace the scheduler with the Engine's pre-configured one.
	// Only mark engineManaged when the Engine actually provides a scheduler so
	// that Server.Start() does not skip starting the fallback scheduler when
	// no Engine scheduler is present.
	if eng.Scheduler != nil {
		server.scheduler = eng.Scheduler
		server.engineManaged = true
	}
	return server
}

func (ds *Server) setDraining(draining bool) {
	ds.tasksMutex.Lock()
	ds.draining = draining
	ds.tasksMutex.Unlock()
}

func (ds *Server) isDraining() bool {
	ds.tasksMutex.RLock()
	defer ds.tasksMutex.RUnlock()
	return ds.draining
}

func (ds *Server) runningTaskCount() int {
	ds.tasksMutex.RLock()
	defer ds.tasksMutex.RUnlock()
	count := 0
	for _, task := range ds.activeTasks {
		if task != nil && task.Status == "running" {
			count++
		}
	}
	return count
}

// Start starts the daemon server
func (ds *Server) Start() error {
	// Create PID file
	if err := ds.createPidFile(); err != nil {
		return fmt.Errorf("failed to create PID file: %w", err)
	}
	defer ds.removePidFile()

	// Start system monitor
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	ds.systemMonitor.Start(monitorCtx)
	logger.Info("System monitor started")
	defer ds.systemMonitor.Stop()

	// Start scheduler only if not managed by Engine
	if ds.scheduler != nil && !ds.engineManaged {
		ds.scheduler.Start()
		defer ds.scheduler.Stop()
	}

	// Sanitize API key to avoid whitespace mismatch
	ds.config.APIKey = strings.TrimSpace(ds.config.APIKey)

	ds.loadSessionsFromDisk()

	// Load historical sessions from OSS (only if enabled)
	if ds.config != nil {
		// config.Get() will return the global config, check OSS enabled there
		globalCfg := config.Get()
		if globalCfg != nil && globalCfg.OSS != nil && globalCfg.OSS.Enabled {
			ds.loadSessionsFromOSS()
		}
	}

	// Setup HTTP router
	router := ds.setupRoutes()

	// Configure HTTP server
	addr := fmt.Sprintf("%s:%d", ds.config.Host, ds.config.Port)
	ds.httpServer = &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	logger.Infof("Starting nano daemon server on %s", addr)
	// 输出当前鉴权状态，帮助排查401问题（不输出密钥内容）
	if ds.config.APIKey == "" {
		logger.Warn("Daemon auth disabled: no API key configured (ws stream is open)")
	} else {
		logger.Infof("Daemon auth enabled: API key configured (length=%d). WS expects api_key query or X-API-Key/Authorization header.", len(ds.config.APIKey))
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		if ds.config.TLSCertFile != "" && ds.config.TLSKeyFile != "" {
			serverErrors <- ds.httpServer.ListenAndServeTLS(ds.config.TLSCertFile, ds.config.TLSKeyFile)
		} else {
			serverErrors <- ds.httpServer.ListenAndServe()
		}
	}()

	// Determine pprof settings from top-level config only
	globalCfg := config.Get()
	pprofEnabled := false
	pprofPort := 0
	if globalCfg != nil {
		pprofEnabled = globalCfg.EnablePprof
		pprofPort = globalCfg.PprofPort
	}
	if pprofEnabled {
		if pprofPort == 0 {
			pprofPort = 6060
		}
		pprofMux := http.NewServeMux()
		pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
		pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		pprofAddr := fmt.Sprintf("%s:%d", "127.0.0.1", pprofPort)
		ds.pprofServer = &http.Server{
			Addr:         pprofAddr,
			Handler:      pprofMux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 0,
			IdleTimeout:  120 * time.Second,
		}

		go func() {
			logger.Infof("Starting pprof server on %s (local-only)", pprofAddr)
			if err := ds.pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Warnf("pprof server error: %v", err)
			}
		}()
	}

	// Wait for interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case <-interrupt:
		logger.Info("Shutdown signal received")
	}

	drainTimeout := daemonDrainTimeout()
	ds.setDraining(true)
	drainDeadline := time.Now().Add(drainTimeout)
	for {
		running := ds.runningTaskCount()
		if running == 0 {
			break
		}
		if time.Now().After(drainDeadline) {
			logger.Warnf("Drain timeout reached with %d running tasks", running)
			break
		}
		logger.Infof("Draining daemon before shutdown, running tasks=%d", running)
		time.Sleep(1 * time.Second)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger.Info("Shutting down daemon server...")

	// Shutdown team-lead registry if initialized
	if ds.teamLeadRegistry != nil {
		if err := ds.teamLeadRegistry.Shutdown(); err != nil {
			logger.Warnf("Error shutting down team-lead registry: %v", err)
		}
	}

	if ds.pprofServer != nil {
		_ = ds.pprofServer.Shutdown(ctx)
	}
	return ds.httpServer.Shutdown(ctx)
}

// setupRoutes configures HTTP routes
func (ds *Server) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Enable CORS if configured
	if ds.config.EnableCORS {
		router.Use(ds.corsMiddleware)
	}

	// Health check (public, no auth required)
	router.HandleFunc("/health", ds.healthHandler).Methods("GET")

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()

	// Add a health check for the v1 API
	api.HandleFunc("/health", ds.healthHandler).Methods("GET")

	// WebSocket route (handles its own authentication) - must be before middleware
	api.HandleFunc("/stream", ds.streamHandler).Methods("GET")
	api.HandleFunc("/teams/sessions/{id}/stream", ds.teamLeadSessionStreamHandler).Methods("GET")

	// Create a separate subrouter for authenticated routes
	authenticatedAPI := api.PathPrefix("").Subrouter()

	// Apply authentication middleware to authenticated routes if API key is configured
	if ds.config.APIKey != "" {
		authenticatedAPI.Use(ds.authMiddleware)
	}

	// Agent status
	authenticatedAPI.HandleFunc("/status", ds.statusHandler).Methods("GET")

	// MCP endpoints
	authenticatedAPI.HandleFunc("/mcp/status", ds.mcpStatusHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/mcp/tools", ds.mcpToolsHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/mcp/diagnostics", ds.mcpDiagnosticsHandler).Methods("GET")

	// Command endpoints
	authenticatedAPI.HandleFunc("/commands", ds.commandsHandler).Methods("GET")

	// Memory endpoints
	authenticatedAPI.HandleFunc("/memory", ds.memoryHandler).Methods("GET", "POST")
	authenticatedAPI.HandleFunc("/memory/{key}", ds.memoryItemHandler).Methods("GET", "DELETE")

	// Monitoring endpoints
	authenticatedAPI.HandleFunc("/metrics", ds.metricsHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/metrics/history", ds.metricsHistoryHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/system/health", ds.systemHealthHandler).Methods("GET")

	// Session management endpoints
	authenticatedAPI.HandleFunc("/sessions", ds.sessionsHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/sessions/reset", ds.resetSessionHandler).Methods("POST")
	authenticatedAPI.HandleFunc("/sessions/{id}", ds.getSessionHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/sessions/{id}", ds.deleteSessionHandler).Methods("DELETE")
	authenticatedAPI.HandleFunc("/sessions/{id}/execute", ds.sessionExecuteHandler).Methods("POST")
	authenticatedAPI.HandleFunc("/sessions/{id}/cancel", ds.cancelSessionPostHandler).Methods("POST")

	// Team-lead session endpoints (if registry is initialized)
	if ds.teamLeadRegistry != nil {
		authenticatedAPI.HandleFunc("/teams/sessions", ds.createTeamLeadSessionHandler).Methods("POST")
		authenticatedAPI.HandleFunc("/teams/sessions", ds.listTeamLeadSessionsHandler).Methods("GET")
		authenticatedAPI.HandleFunc("/teams/sessions/{id}", ds.getTeamLeadSessionHandler).Methods("GET")
		authenticatedAPI.HandleFunc("/teams/sessions/{id}", ds.deleteTeamLeadSessionHandler).Methods("DELETE")
		authenticatedAPI.HandleFunc("/teams/sessions/{id}/execute", ds.executeInTeamLeadSessionHandler).Methods("POST")
		authenticatedAPI.HandleFunc("/teams/sessions/{id}/cancel", ds.cancelTeamLeadSessionHandler).Methods("POST")
		authenticatedAPI.HandleFunc("/teams/sessions/{id}/events", ds.teamLeadSessionEventsHandler).Methods("GET")
	}

	// Scheduler endpoints
	authenticatedAPI.HandleFunc("/scheduler/tasks", ds.scheduleTaskHandler).Methods("POST")
	authenticatedAPI.HandleFunc("/scheduler/tasks", ds.listTasksHandler).Methods("GET")
	authenticatedAPI.HandleFunc("/scheduler/tasks/{id}", ds.deleteTaskHandler).Methods("DELETE")

	if strings.TrimSpace(os.Getenv("NANO_DAEMON_LOG_ROUTES")) == "true" {
		_ = router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
			tpl, err := route.GetPathTemplate()
			if err != nil || tpl == "" {
				return nil
			}
			if !strings.Contains(tpl, "/api/v1") && !strings.Contains(tpl, "/sessions") {
				return nil
			}
			methods, _ := route.GetMethods()
			if len(methods) == 0 {
				logger.Infof("Route: %s", tpl)
				return nil
			}
			logger.Infof("Route: %s %s", strings.Join(methods, ","), tpl)
			return nil
		})
	}

	// Static file server for web UI (optional)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./web/")))

	return router
}

// Middleware functions
func (ds *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (ds *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ds.config.APIKey != "" {
			expected := strings.TrimSpace(ds.config.APIKey)
			apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if apiKey == "" {
				// Also support Authorization: Bearer <token>
				auth := r.Header.Get("Authorization")
				if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
					apiKey = strings.TrimSpace(auth[7:])
				}
			}
			if apiKey != expected {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (ds *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	uptime := time.Since(ds.startTime).Seconds()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   version.Version,
		"uptime":    uptime,
	})
}

func (ds *Server) statusHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	agentStatus := "running"
	mcpEnabled := false
	activeTools := 0
	// Try to derive information from agent and toolbox if available
	if ds.agent != nil && ds.agent.GetToolbox() != nil {
		toolbox := ds.agent.GetToolbox()
		mcpEnabled = toolbox.IsMCPEnabled()
		if tools := toolbox.List(); tools != nil {
			activeTools = len(tools)
		}
	}

	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"agent_status": agentStatus,
		"mcp_enabled":  mcpEnabled,
		"memory_size":  0,
		"active_tools": activeTools,
	})
}

func (ds *Server) generateRunID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("run_%s", hex.EncodeToString(bytes))
}

func (ds *Server) generateSessionID() string { //nolint:unused
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("sess_%s", hex.EncodeToString(bytes))
}

// sessionsHandler handles GET /api/v1/sessions - lists sessions
func (ds *Server) sessionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if ds.agent == nil {
		http.Error(w, "agent not initialized", http.StatusInternalServerError)
		return
	}

	sm := ds.agent.GetSessionManager()
	if sm == nil {
		http.Error(w, "session manager not initialized", http.StatusInternalServerError)
		return
	}

	type historyItem struct {
		ID           string  `json:"id"`
		Type         string  `json:"type"` // "session"
		Title        string  `json:"title,omitempty"`
		CreatedAt    string  `json:"created_at"` // ISO8601
		LastActiveAt string  `json:"last_active_at,omitempty"`
		Status       string  `json:"status,omitempty"`
		Stored       bool    `json:"stored"`
		Active       bool    `json:"active"`
		TotalTokens  int     `json:"total_tokens,omitempty"`
		Duration     float64 `json:"duration,omitempty"`
		MessageCount int     `json:"message_count"`
		sortTime     time.Time
	}

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}

	allItems := make([]*historyItem, 0, 256)
	itemsMap := make(map[string]*historyItem)

	addOrUpdateItem := func(item *historyItem) {
		if item == nil || item.ID == "" {
			return
		}
		if existing, ok := itemsMap[item.ID]; ok {
			if item.Title != "" {
				existing.Title = item.Title
			}
			if item.CreatedAt != "" {
				existing.CreatedAt = item.CreatedAt
			}
			if item.LastActiveAt != "" {
				existing.LastActiveAt = item.LastActiveAt
			}
			if item.Status != "" {
				existing.Status = item.Status
			}
			if item.Stored {
				existing.Stored = true
			}
			if item.Active {
				existing.Active = true
			}
			if item.TotalTokens > 0 {
				existing.TotalTokens = item.TotalTokens
			}
			if item.Duration > 0 {
				existing.Duration = item.Duration
			}
			if item.MessageCount > 0 {
				existing.MessageCount = item.MessageCount
			}
			if item.sortTime.After(existing.sortTime) {
				existing.sortTime = item.sortTime
			}
			return
		}
		itemsMap[item.ID] = item
		allItems = append(allItems, item)
	}

	for _, s := range sm.ListSessions() {
		title := ""
		if val, ok := s.Metadata["title"]; ok {
			title, _ = val.(string)
		}
		status := ""
		if val, ok := s.Metadata["status"]; ok {
			status, _ = val.(string)
		}
		ds.tasksMutex.RLock()
		if t := ds.activeTasks[s.ID]; t != nil && t.Status != "" {
			status = t.Status
			if title == "" {
				if t.Title != "" {
					title = t.Title
				} else if t.Command != "" {
					title = t.Command
				}
			}
		}
		ds.tasksMutex.RUnlock()

		createdAt := s.CreatedAt
		if createdAt.IsZero() {
			createdAt = s.LastActiveAt
		}

		addOrUpdateItem(&historyItem{
			ID:           s.ID,
			Type:         "session",
			Title:        title,
			CreatedAt:    createdAt.Format(time.RFC3339),
			LastActiveAt: s.LastActiveAt.Format(time.RFC3339),
			Status:       status,
			Stored:       false,
			Active:       true,
			TotalTokens:  s.TotalTokens,
			Duration:     s.Duration,
			MessageCount: len(s.GetConversationHistory()),
			sortTime:     s.LastActiveAt,
		})
	}

	if storedInfos, err := sm.ListStoredSessionInfos(); err == nil {
		for _, info := range storedInfos {
			tm, parseErr := time.Parse(time.RFC3339, info.UpdatedAt)
			if parseErr != nil {
				tm = time.Now()
			}
			addOrUpdateItem(&historyItem{
				ID:        info.ID,
				Type:      "session",
				Title:     info.Title,
				CreatedAt: tm.Format(time.RFC3339),
				Stored:    true,
				Active:    false,
				sortTime:  tm,
			})
		}
	}

	ds.tasksMutex.RLock()
	for sid, t := range ds.activeTasks {
		if t == nil {
			continue
		}
		title := t.Title
		if title == "" {
			title = t.Command
		}
		lastAt := t.EndTime
		if lastAt.IsZero() {
			lastAt = time.Now()
		}
		duration := lastAt.Sub(t.StartTime).Seconds()
		totalTokens := 0
		if t.TokenUsage != nil {
			totalTokens = t.TokenUsage.TotalTokens
		}
		addOrUpdateItem(&historyItem{
			ID:           sid,
			Type:         "session",
			Title:        title,
			CreatedAt:    t.StartTime.Format(time.RFC3339),
			LastActiveAt: lastAt.Format(time.RFC3339),
			Status:       t.Status,
			Stored:       false,
			Active:       t.Status == "running",
			TotalTokens:  totalTokens,
			Duration:     duration,
			MessageCount: 0,
			sortTime:     lastAt,
		})
	}
	ds.tasksMutex.RUnlock()

	// Sort all by time descending
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].sortTime.After(allItems[j].sortTime)
	})

	if len(allItems) > limit {
		allItems = allItems[:limit]
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"sessions": allItems,
	})
}

// getSessionHandler handles GET /api/v1/sessions/{id} - gets session details
func (ds *Server) getSessionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	if ds.agent == nil {
		http.Error(w, "agent not initialized", http.StatusInternalServerError)
		return
	}

	sm := ds.agent.GetSessionManager()
	if sm == nil {
		http.Error(w, "session manager not initialized", http.StatusInternalServerError)
		return
	}

	filterHistory := func(history []llm.Message) []llm.Message {
		if len(history) == 0 {
			return history
		}
		filtered := make([]llm.Message, 0, len(history))
		for _, msg := range history {
			if msg.Role == "system" {
				continue
			}
			filtered = append(filtered, msg)
		}
		return filtered
	}

	// 1. Try generic Session
	session, exists := sm.GetSession(sessionID)
	if exists {
		history := filterHistory(session.GetConversationHistory())
		metadata := session.GetMetadataCopy()
		if data, err := os.ReadFile(filepath.Join(getRuntimeSessionsDir(), sessionID, "metadata.json")); err == nil {
			var runtimeMeta map[string]interface{}
			if json.Unmarshal(data, &runtimeMeta) == nil {
				for k, v := range runtimeMeta {
					metadata[k] = v
				}
			}
		}
		metadata["type"] = "session"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             session.ID,
			"created_at":     session.CreatedAt,
			"last_active_at": session.LastActiveAt,
			"metadata":       metadata,
			"history":        history,
			"success":        true,
		})
		return
	}
	http.Error(w, "session not found", http.StatusNotFound)
}

func (ds *Server) sessionExecuteHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if ds.isDraining() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "daemon is draining and not accepting new executions",
		})
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["id"]
	if sessionID == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}

	if ds.agent == nil {
		http.Error(w, "agent not initialized", http.StatusInternalServerError)
		return
	}

	var req struct {
		Command      string                `json:"command"`
		Timeout      int                   `json:"timeout,omitempty"`
		IncludeSteps bool                  `json:"include_steps,omitempty"`
		Async        bool                  `json:"async,omitempty"`
		Images       []llm.MultimodalImage `json:"images,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	task, err := ds.startUnifiedTask(req.Command, sessionID, req.Timeout, req.Images)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	if req.Async {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":    true,
			"session_id": sessionID,
			"run_id":     task.ID,
			"message":    "Session execution started",
			"status":     "running",
		})
		return
	}

	ch := task.Broadcaster.Subscribe()
	defer task.Broadcaster.Unsubscribe(ch)

	var resultBuilder strings.Builder
	var lastError string
	var steps []event.StreamEvent
	var lastTokenStats *event.TokenStats
	var currentSessionID = sessionID
	var completed bool

	waitTimeout := req.Timeout
	waitTimeout = NormalizeTaskTimeoutSeconds(waitTimeout)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(waitTimeout+ClientHTTPGraceSeconds)*time.Second)
	defer cancel()

Loop:
	for {
		select {
		case <-ctx.Done():
			lastError = "request timed out or cancelled"
			break Loop
		case ev, ok := <-ch:
			if !ok {
				break Loop
			}

			if ev.Type == event.EventTypeStreamContent ||
				(ev.Type == event.EventTypeContent && ev.Source != "llm_client") {
				if ev.Content != "" {
					resultBuilder.WriteString(ev.Content)
				}
			}
			if ev.Type == event.EventTypeError {
				lastError = ev.Error
			}
			if ev.Type == event.EventTypeTaskCompletion {
				completed = true
			}
			if ev.Type == event.EventTypeTokenStats && ev.TokenStats != nil {
				lastTokenStats = ev.TokenStats
			}
			if ev.Type == event.EventTypeSessionInfo && ev.SessionID != "" {
				currentSessionID = ev.SessionID
			}

			if req.IncludeSteps {
				switch ev.Type {
				case event.EventTypeTokenStats, event.EventTypeDebug, event.EventTypeSatisfactionEval:
				default:
					steps = append(steps, ev)
				}
			}

			if ev.Type == event.EventTypeTaskCompletion || ev.Type == event.EventTypeError {
				break Loop
			}
		}
	}

	if lastError != "" {
		response := map[string]any{
			"success":    false,
			"error":      lastError,
			"session_id": currentSessionID,
		}
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	response := map[string]any{
		"success":     true,
		"result":      resultBuilder.String(),
		"token_stats": lastTokenStats,
		"status":      task.Status,
		"completed":   completed,
		"session_id":  currentSessionID,
		"run_id":      task.ID,
	}
	if req.IncludeSteps {
		response["steps"] = steps
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (ds *Server) cancelSessionPostHandler(w http.ResponseWriter, r *http.Request) {
	ds.cancelSessionHandler(w, r)
}

// resetSessionHandler handles POST /api/v1/sessions/reset by clearing a
// session's conversation history, metadata (including title), and stats
// while preserving its ID and CreatedAt for continued use.
func (ds *Server) resetSessionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		SessionID string `json:"session_id"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   "invalid request body",
			})
			return
		}
	}

	if req.SessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "session_id required",
		})
		return
	}

	if ds.agent == nil || ds.agent.GetSessionManager() == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "session manager unavailable",
		})
		return
	}

	sm := ds.agent.GetSessionManager()
	session, exists := sm.GetSession(req.SessionID)
	if !exists || session == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "session not found",
		})
		return
	}

	session.SetConversationHistory([]llm.Message{})
	session.ClearMetadata()
	if err := sm.SaveSession(req.SessionID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   fmt.Sprintf("failed to save session: %v", err),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"session_id": req.SessionID,
		"status":     "reset",
	})
}

// cancelSessionHandler handles PUT /api/v1/session/{id} - cancels a session (task)
func (ds *Server) cancelSessionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	ds.tasksMutex.RLock()
	task, exists := ds.activeTasks[id]
	ds.tasksMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "session not found",
		})
		return
	}

	if task.Status != "running" {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   fmt.Sprintf("task is not running (current status: %s)", task.Status),
		})
		return
	}

	if task.Cancel == nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "cannot cancel this session",
		})
		return
	}

	// Cancel the task
	task.Cancel()

	ds.tasksMutex.Lock()
	task.Status = "cancelled"
	task.EndTime = time.Now()
	if task.Scribe != nil {
		_ = task.Scribe.SaveMetadata(map[string]interface{}{
			"id":          id,
			"command":     task.Command,
			"status":      task.Status,
			"start_time":  task.StartTime.Format(time.RFC3339),
			"end_time":    task.EndTime.Format(time.RFC3339),
			"duration":    task.EndTime.Sub(task.StartTime).Seconds(),
			"token_usage": task.TokenUsage,
			"updated_at":  task.EndTime.Format(time.RFC3339),
			"title":       task.Title,
		})
		task.Scribe.Sync()
	}
	ds.tasksMutex.Unlock()

	if ds.agent != nil && ds.agent.GetSessionManager() != nil {
		sm := ds.agent.GetSessionManager()
		if _, exists := sm.GetSession(id); exists {
			_ = sm.SaveSession(id)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"session_id": id,
		"status":     "cancelled",
	})
}

// deleteSessionHandler handles DELETE /api/v1/sessions/{id} - deletes a session
func (ds *Server) deleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	ds.tasksMutex.Lock()
	if t, ok := ds.activeTasks[id]; ok && t != nil {
		if t.Status == "running" && t.Cancel != nil {
			t.Cancel()
		}
		if t.Scribe != nil {
			t.Scribe.Sync()
		}
		delete(ds.activeTasks, id)
	}
	ds.tasksMutex.Unlock()

	_ = os.RemoveAll(filepath.Join(getRuntimeSessionsDir(), id))

	if ds.agent != nil && ds.agent.GetSessionManager() != nil {
		if _, err := ds.agent.GetSessionManager().DeleteSession(id); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   fmt.Sprintf("failed to delete session from storage: %v", err),
				"id":      id,
			})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "session deleted",
		"id":      id,
	})
}

func (ds *Server) loadSessionsFromDisk() {
	sessionsDir := getRuntimeSessionsDir()
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		logger.Warnf("Failed to create sessions directory: %v", err)
		return
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		logger.Warnf("Failed to read sessions directory: %v", err)
		return
	}

	count := 0
	ds.tasksMutex.Lock()
	defer ds.tasksMutex.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		metaPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}

		var meta map[string]interface{}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		status, _ := meta["status"].(string)
		cmd, _ := meta["command"].(string)
		title, _ := meta["title"].(string)
		startTimeStr, _ := meta["start_time"].(string)
		startTime, _ := time.Parse(time.RFC3339, startTimeStr)
		createdAtStr, _ := meta["created_at"].(string)
		if createdAtStr != "" {
			if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
				startTime = t
			}
		}

		var endTime time.Time
		if endTimeStr, ok := meta["end_time"].(string); ok && endTimeStr != "" {
			endTime, _ = time.Parse(time.RFC3339, endTimeStr)
		}
		if updatedAtStr, ok := meta["updated_at"].(string); ok && updatedAtStr != "" {
			if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
				endTime = t
			}
		}

		var tokenStats *event.TokenStats
		if usageData, ok := meta["token_usage"]; ok {
			if usageMap, ok := usageData.(map[string]interface{}); ok {
				if b, err := json.Marshal(usageMap); err == nil {
					_ = json.Unmarshal(b, &tokenStats)
				}
			}
		}

		if status == "running" {
			status = "error"
			meta["status"] = "error"
			if endTime.IsZero() {
				endTime = time.Now()
				meta["updated_at"] = endTime.Format(time.RFC3339)
			}
			if encoded, err := json.MarshalIndent(meta, "", "  "); err == nil {
				tmpPath := metaPath + ".tmp"
				if err := os.WriteFile(tmpPath, encoded, 0644); err == nil {
					_ = os.Rename(tmpPath, metaPath)
				}
			}
		}

		if _, exists := ds.activeTasks[sessionID]; exists {
			continue
		}
		ds.activeTasks[sessionID] = &ActiveTask{
			ID:          getString(meta, "run_id"),
			SessionID:   sessionID,
			Command:     cmd,
			Title:       title,
			Type:        "unified",
			StartTime:   startTime,
			EndTime:     endTime,
			Status:      status,
			TokenUsage:  tokenStats,
			Broadcaster: NewEventBroadcaster(),
			Store:       NewTaskEventStore(5000),
		}
		count++
	}

	if count > 0 {
		logger.Infof("Loaded %d historical sessions from disk", count)
	}
}

func (ds *Server) loadSessionsFromOSS() {
	go func() {
		cfg := config.Get().OSS
		if cfg == nil || !cfg.Enabled {
			return
		}

		client, err := oss.New(cfg.NormalizedEndpoint(), cfg.AccessKeyID, cfg.AccessKeySecret)
		if err != nil {
			logger.Warnf("Failed to create OSS client: %v", err)
			return
		}
		bucket, err := client.Bucket(cfg.DefaultBucket)
		if err != nil {
			logger.Warnf("Failed to get OSS bucket: %v", err)
			return
		}

		marker := ""
		loaded := 0
		ds.tasksMutex.Lock()
		defer ds.tasksMutex.Unlock()
		for {
			resp, err := bucket.ListObjects(oss.Prefix("sessions/"), oss.Marker(marker), oss.MaxKeys(500))
			if err != nil {
				logger.Warnf("OSS list objects failed: %v", err)
				break
			}
			for _, obj := range resp.Objects {
				if !strings.HasSuffix(obj.Key, "/metadata.json") {
					continue
				}
				parts := strings.Split(strings.TrimPrefix(obj.Key, "sessions/"), "/")
				if len(parts) < 2 {
					continue
				}
				sessionID := parts[0]
				if _, exists := ds.activeTasks[sessionID]; exists {
					continue
				}
				rc, err := bucket.GetObject(obj.Key)
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				_ = rc.Close()
				if err != nil {
					continue
				}
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err != nil {
					continue
				}
				status, _ := meta["status"].(string)
				cmd, _ := meta["command"].(string)
				title, _ := meta["title"].(string)
				startTimeStr, _ := meta["start_time"].(string)
				startTime, _ := time.Parse(time.RFC3339, startTimeStr)
				var endTime time.Time
				if endTimeStr, ok := meta["end_time"].(string); ok && endTimeStr != "" {
					endTime, _ = time.Parse(time.RFC3339, endTimeStr)
				}
				if updatedAtStr, ok := meta["updated_at"].(string); ok && updatedAtStr != "" {
					if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
						endTime = t
					}
				}
				var tokenStats *event.TokenStats
				if usageData, ok := meta["token_usage"]; ok {
					if usageMap, ok := usageData.(map[string]interface{}); ok {
						if b, err := json.Marshal(usageMap); err == nil {
							_ = json.Unmarshal(b, &tokenStats)
						}
					}
				}
				if status == "running" {
					status = "error"
					meta["status"] = "error"
					if endTime.IsZero() {
						endTime = time.Now()
						meta["updated_at"] = endTime.Format(time.RFC3339)
					}
					buf := new(bytes.Buffer)
					enc := json.NewEncoder(buf)
					enc.SetIndent("", "  ")
					if enc.Encode(meta) == nil {
						_ = bucket.PutObject(fmt.Sprintf("sessions/%s/metadata.json", sessionID), buf)
					}
				}
				ds.activeTasks[sessionID] = &ActiveTask{
					ID:          getString(meta, "run_id"),
					SessionID:   sessionID,
					Command:     cmd,
					Title:       title,
					Type:        "unified",
					StartTime:   startTime,
					EndTime:     endTime,
					Status:      status,
					TokenUsage:  tokenStats,
					Broadcaster: NewEventBroadcaster(),
					Store:       NewTaskEventStore(5000),
				}
				loaded++
			}
			if resp.IsTruncated {
				marker = resp.NextMarker
			} else {
				break
			}
		}
		if loaded > 0 {
			logger.Infof("Loaded %d historical sessions from OSS", loaded)
		}
	}()
}

// startUnifiedTask starts a task in background that can be attached to via WebSocket
func (ds *Server) startUnifiedTask(command, sessionID string, timeout int, images []llm.MultimodalImage) (*ActiveTask, error) {
	runID := ds.generateRunID()

	scribe, err := NewSessionScribe(sessionID)
	if err != nil {
		logger.Errorf("Failed to create session scribe: %v", err)
	}

	timeout = NormalizeTaskTimeoutSeconds(timeout)

	// Create context with timeout
	// This context controls the task execution lifetime
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)

	// Preserve existing session title if present, before registering the task
	var preservedTitle string
	if ds.agent != nil && ds.agent.GetSessionManager() != nil && sessionID != "" {
		if s, ok := ds.agent.GetSessionManager().GetSession(sessionID); ok && s != nil {
			if v, has := s.GetMetadata("title"); has {
				if ts, ok := v.(string); ok {
					preservedTitle = ts
				}
			}
		}
	}

	// Create active task
	task := &ActiveTask{
		ID:          runID,
		SessionID:   sessionID,
		Command:     command,
		Images:      images,
		Type:        "unified",
		Title:       preservedTitle,
		StartTime:   time.Now(),
		Cancel:      cancel,
		Status:      "running",
		Scribe:      scribe,
		Broadcaster: NewEventBroadcaster(),
		Store:       NewTaskEventStore(5000),
	}

	// Register task
	ds.tasksMutex.Lock()
	if existing, ok := ds.activeTasks[sessionID]; ok && existing != nil && existing.Status == "running" {
		ds.tasksMutex.Unlock()
		if cancel != nil {
			cancel()
		}
		if scribe != nil {
			scribe.Close()
		}
		return nil, fmt.Errorf("session %s already has a running execution", sessionID)
	}
	ds.activeTasks[sessionID] = task
	ds.tasksMutex.Unlock()

	// Persist initial metadata
	if scribe != nil {
		meta := map[string]interface{}{
			"id":         sessionID,
			"command":    command,
			"status":     "running",
			"start_time": task.StartTime.Format(time.RFC3339),
			"type":       "unified",
			"title":      preservedTitle,
			"updated_at": time.Now().Format(time.RFC3339),
			"run_id":     runID,
		}
		_ = scribe.SaveMetadata(meta)
		taskStartEvent := event.StreamEvent{
			Type:      event.EventTypeTaskStart,
			SessionID: sessionID,
			Content:   fmt.Sprintf("Session started: %s", command),
			Timestamp: time.Now().Unix(),
		}
		if len(images) > 0 {
			taskStartEvent.Metadata = map[string]interface{}{
				"images": sanitizeImages(images),
			}
		}
		_ = scribe.WriteEvent(taskStartEvent)
	}

	if ds.agent != nil && ds.agent.GetSessionManager() != nil {
		sm := ds.agent.GetSessionManager()
		if _, exists := sm.GetSession(sessionID); exists {
			_ = sm.SaveSession(sessionID)
		}
	}

	// Start async title generation
	go func() {
		// If session already has a stable (non-default) title, skip regeneration
		if ds.agent != nil && ds.agent.GetSessionManager() != nil && sessionID != "" {
			if s, ok := ds.agent.GetSessionManager().GetSession(sessionID); ok && s != nil {
				if v, has := s.GetMetadata("title"); has {
					if ts, ok := v.(string); ok {
						if ts != "" && ts != "New Chat" && ts != "新会话" && !strings.HasPrefix(ts, "Chat ") {
							return
						}
					}
				}
			}
		}

		// Construct history for title generation
		history := []llm.Message{{Role: "user", Content: command}}
		// Add images context if present
		for _, img := range images {
			history = append(history, llm.Message{Role: "user", Content: fmt.Sprintf("[Image: %s]", img.URL)})
		}

		title, err := ds.agent.GenerateTitle(history)
		if err != nil {
			logger.Warnf("Failed to generate task title: %v", err)
			return
		}

		if title != "" {
			ds.tasksMutex.Lock()
			if t, ok := ds.activeTasks[sessionID]; ok {
				t.Title = title

				// Persist updated metadata
				if t.Scribe != nil {
					currentMeta := map[string]interface{}{
						"id":         t.SessionID,
						"command":    t.Command,
						"status":     t.Status,
						"start_time": t.StartTime.Format(time.RFC3339),
						"type":       t.Type,
						"title":      t.Title,
						"updated_at": time.Now().Format(time.RFC3339),
						"run_id":     runID,
					}
					// If we have token usage or end time (unlikely this early), add them
					if !t.EndTime.IsZero() {
						currentMeta["end_time"] = t.EndTime.Format(time.RFC3339)
						currentMeta["duration"] = t.EndTime.Sub(t.StartTime).Seconds()
					}

					_ = t.Scribe.SaveMetadata(currentMeta)
				}

				// Only broadcast title if it's not overriding an existing stable title
				shouldBroadcast := true
				if ds.agent != nil && ds.agent.GetSessionManager() != nil && sessionID != "" {
					if s, ok := ds.agent.GetSessionManager().GetSession(sessionID); ok && s != nil {
						if v, has := s.GetMetadata("title"); has {
							if ts, ok := v.(string); ok {
								if ts != "" && ts != "New Chat" && ts != "新会话" && !strings.HasPrefix(ts, "Chat ") {
									shouldBroadcast = false
								}
							}
						}
					}
				}
				if shouldBroadcast {
					updateEvent := event.StreamEvent{
						Type:      event.EventTypeSessionInfo,
						SessionID: t.SessionID,
						Title:     title,
						Timestamp: time.Now().Unix(),
						Metadata:  map[string]interface{}{"title": title},
					}
					t.Broadcaster.Publish(updateEvent)
					logger.Infof("Generated and broadcasted title for run %s: %s", runID, title)
				}
			}
			ds.tasksMutex.Unlock()
		}
	}()

	// Start execution in background goroutine
	go func() {
		defer cancel()
		if scribe != nil {
			defer func() { scribe.Close() }()
		}

		var lastTokenStats *event.TokenStats
		var currentSessionID = sessionID
		taskKey := sessionID
		var taskCompleted bool
		var seq int64

		handler := func(streamEvent event.StreamEvent) {
			// Augment event
			if streamEvent.Timestamp == 0 {
				streamEvent.Timestamp = time.Now().Unix()
			}
			if streamEvent.SessionID == "" && currentSessionID != "" {
				streamEvent.SessionID = currentSessionID
			}
			if streamEvent.RunID == "" {
				streamEvent.RunID = runID
			}
			seq++
			streamEvent.Seq = seq

			// Always assign a unique ID if it doesn't have one
			if streamEvent.ID == "" {
				streamEvent.ID = fmt.Sprintf("%s-%d-%d", streamEvent.RunID, streamEvent.Seq, streamEvent.Timestamp)
			}

			if streamEvent.Priority == "" {
				switch streamEvent.Type {
				case event.EventTypeError,
					event.EventTypeTaskCompletion,
					event.EventTypeTaskCancel,
					event.EventTypePlannerPlanSnapshot,
					event.EventTypePlannerPlanUpdate,
					event.EventTypePlannerDecision,
					event.EventTypeExecutorState,
					event.EventTypeExecutorSchedule:
					streamEvent.Priority = "high"
				case event.EventTypeStreamContent, event.EventTypeWorkerLog:
					streamEvent.Priority = "low"
				default:
					streamEvent.Priority = "normal"
				}
			}

			// Track state
			if streamEvent.Type == event.EventTypeTokenStats && streamEvent.TokenStats != nil {
				lastTokenStats = streamEvent.TokenStats
			}
			if streamEvent.Type == event.EventTypeSessionInfo && streamEvent.SessionID != "" {
				currentSessionID = streamEvent.SessionID
				ds.tasksMutex.Lock()
				if t, ok := ds.activeTasks[taskKey]; ok {
					if taskKey != currentSessionID {
						delete(ds.activeTasks, taskKey)
						ds.activeTasks[currentSessionID] = t
						taskKey = currentSessionID
					}
					t.SessionID = currentSessionID
				}
				ds.tasksMutex.Unlock()
			}
			if streamEvent.Type == event.EventTypeTaskCompletion {
				taskCompleted = true
			}

			if task.Store != nil {
				task.Store.Add(streamEvent)
			}

			// 1. Write to Scribe (Persistence)
			if scribe != nil {
				_ = scribe.WriteEvent(streamEvent)
			}

			// 2. Broadcast to Subscribers (Real-time)
			task.Broadcaster.Publish(streamEvent)
		}

		// Run Agent
		err := ds.agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, command, images, handler)

		// Finalize Task Status
		ds.tasksMutex.Lock()
		if t, exists := ds.activeTasks[taskKey]; exists {
			t.EndTime = time.Now()
			t.TokenUsage = lastTokenStats

			if err != nil {
				switch err {
				case context.Canceled:
					t.Status = "cancelled"
				case context.DeadlineExceeded:
					t.Status = "timeout"
				default:
					t.Status = "error"
				}
				// Broadcast error event
				errEvent := event.StreamEvent{
					Type:      event.EventTypeError,
					Error:     err.Error(),
					SessionID: currentSessionID,
					RunID:     runID,
					Timestamp: time.Now().Unix(),
					Priority:  "high",
				}
				seq++
				errEvent.Seq = seq
				if scribe != nil {
					_ = scribe.WriteEvent(errEvent)
				}
				if t.Store != nil {
					t.Store.Add(errEvent)
				}
				t.Broadcaster.Publish(errEvent)
			} else if taskCompleted {
				t.Status = "completed"
			} else {
				t.Status = "incomplete"
			}

			// Save final metadata
			if scribe != nil {
				finalMeta := map[string]interface{}{
					"id":          currentSessionID,
					"command":     command,
					"status":      t.Status,
					"title":       t.Title,
					"start_time":  t.StartTime.Format(time.RFC3339),
					"end_time":    t.EndTime.Format(time.RFC3339),
					"duration":    t.EndTime.Sub(t.StartTime).Seconds(),
					"token_usage": t.TokenUsage,
					"updated_at":  time.Now().Format(time.RFC3339),
					"run_id":      runID,
				}
				_ = scribe.SaveMetadata(finalMeta)
			}
		}
		ds.tasksMutex.Unlock()

		// Ensure session persistence consistency at completion
		if ds.agent != nil && ds.agent.GetSessionManager() != nil {
			sm := ds.agent.GetSessionManager()
			// Only save if session still exists - don't resurrect deleted sessions
			if session, exists := sm.GetSession(currentSessionID); exists {
				// Make sure we save it to disk/OSS
				_ = sm.SaveSession(currentSessionID)
				logger.Infof("Saved session %s history to storage upon completion (history length: %d)",
					currentSessionID, len(session.GetConversationHistory()))
			}
		}

		// Schedule cleanup of finished task entry from memory
		ds.scheduleTaskCleanup(taskKey, 2*time.Minute)
	}()

	return task, nil
}

func (ds *Server) streamTaskEvents(clientCtx context.Context, task *ActiveTask, connManager *ConnectionManager) error {
	return ds.streamTaskEventsResumable(clientCtx, task, connManager, 0, nil)
}

// isSequenced reports whether ev carries a valid (non-zero) sequence number
// that can be used for deduplication between the replay path and the live channel.
func isSequenced(ev event.StreamEvent) bool {
	return ev.Seq > 0
}

func (ds *Server) streamTaskEventsResumable(clientCtx context.Context, task *ActiveTask, connManager *ConnectionManager, sinceSeq int64, streams []string) error {
	filter := ds.buildStreamFilter(streams)
	lastSeq := int64(0)
	if task.Store != nil {
		lastSeq = task.Store.LastSeq()
	}

	// Subscribe before sending session_start so events published
	// in the window before the select loop are not missed due to
	// subscribing too late. This does not guarantee delivery if the
	// broadcaster drops events under backpressure or buffer overflow.
	var subCh chan event.StreamEvent
	var replayCutoffSeq int64
	if task.Status == "running" {
		if task.Broadcaster == nil {
			return fmt.Errorf("running task %s has nil broadcaster; task may not be properly initialized", task.ID)
		}
		subCh = task.Broadcaster.Subscribe()
		defer task.Broadcaster.Unsubscribe(subCh)
		if task.Store != nil {
			replayCutoffSeq = task.Store.LastSeq()
		}
	}

	sessionStartMsg := map[string]interface{}{
		"type":       "session_start",
		"session_id": task.SessionID,
		"run_id":     task.ID,
		"command":    task.Command,
		"status":     task.Status,
		"since_seq":  sinceSeq,
		"last_seq":   lastSeq,
	}
	if len(task.Images) > 0 {
		sessionStartMsg["images"] = sanitizeImages(task.Images)
	}
	_ = connManager.SafeWriteJSON(sessionStartMsg)

	if task.Store != nil {
		if sinceSeq >= 0 {
			for _, ev := range task.Store.Since(sinceSeq, filter) {
				// For running tasks, skip events beyond the cutoff captured at
				// subscribe time; they will arrive via the live channel instead.
				if subCh != nil && isSequenced(ev) && ev.Seq > replayCutoffSeq {
					continue
				}
				if ev.Priority == "low" {
					_ = connManager.SafeWriteJSONAsync(ev)
					continue
				}
				if err := connManager.SafeWriteJSON(ev); err != nil {
					logger.Errorf("Failed to write replay to WS: %v", err)
					return err
				}
			}
		}
	}

	if task.Status != "running" {
		return nil
	}

	for {
		select {
		case <-clientCtx.Done():
			return nil
		case ev, ok := <-subCh:
			if !ok {
				return nil
			}

			// Skip events already sent in the store replay.
			if isSequenced(ev) && ev.Seq <= replayCutoffSeq {
				continue
			}

			if !filter(ev) {
				continue
			}

			if ev.Priority == "low" {
				_ = connManager.SafeWriteJSONAsync(ev)
				continue
			}

			if err := connManager.SafeWriteJSON(ev); err != nil {
				logger.Errorf("Failed to write to WS: %v", err)
				return err
			}

			if ev.Type == event.EventTypeTaskCompletion || ev.Type == event.EventTypeError {
				return nil
			}
		}
	}
}

func (ds *Server) buildStreamFilter(streams []string) func(event.StreamEvent) bool {
	streamSet := map[string]struct{}{}
	for _, s := range streams {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		streamSet[s] = struct{}{}
	}
	hasFilter := len(streamSet) > 0

	return func(ev event.StreamEvent) bool {
		if ev.Type == event.EventTypeTokenStats ||
			ev.Type == event.EventTypeDebug ||
			ev.Type == event.EventTypeSatisfactionEval ||
			ev.Type == event.EventTypeContent {
			return false
		}

		if !hasFilter {
			return true
		}

		switch ev.Type {
		case event.EventTypePlannerPlanSnapshot, event.EventTypePlannerPlanUpdate, event.EventTypePlannerDecision:
			_, ok := streamSet["planner"]
			return ok
		case event.EventTypeExecutorState, event.EventTypeExecutorSchedule, event.EventTypeWaitingForUser:
			_, ok := streamSet["executor"]
			return ok
		case event.EventTypeWorkerStart, event.EventTypeWorkerUpdate, event.EventTypeWorkerLog, event.EventTypeWorkerEnd:
			_, ok := streamSet["worker"]
			return ok
		case event.EventTypeToolCall, event.EventTypeToolResult, event.EventTypeToolUse:
			_, ok := streamSet["tool"]
			return ok
		default:
			_, ok := streamSet["content"]
			return ok
		}
	}
}

func (ds *Server) loadTaskFromDisk(sessionID string) (*ActiveTask, error) {
	sessionDir := filepath.Join(getRuntimeSessionsDir(), sessionID)
	metaPath := filepath.Join(sessionDir, "metadata.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to load from OSS
			cfg := config.Get().OSS
			if cfg != nil && cfg.Enabled {
				client, ossErr := oss.New(cfg.NormalizedEndpoint(), cfg.AccessKeyID, cfg.AccessKeySecret)
				if ossErr == nil {
					bucket, ossErr := client.Bucket(cfg.DefaultBucket)
					if ossErr == nil {
						ossKey := fmt.Sprintf("sessions/%s/metadata.json", sessionID)
						rc, ossErr := bucket.GetObject(ossKey)
						if ossErr == nil {
							data, err = io.ReadAll(rc)
							_ = rc.Close()
							if err == nil {
								logger.Infof("Downloaded metadata.json from OSS for session %s", sessionID)
								// Save locally
								_ = os.MkdirAll(sessionDir, 0755)
								_ = os.WriteFile(metaPath, data, 0644)
							}
						}
					}
				}
			}
		}

		if len(data) == 0 {
			return nil, fmt.Errorf("metadata not found locally or in OSS")
		}
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	runID := getString(meta, "run_id")
	if runID == "" {
		runID = getString(meta, "id")
	}

	cmd := getString(meta, "command")
	title := getString(meta, "title")
	status := getString(meta, "status")

	startTimeStr := getString(meta, "start_time")
	startTime, _ := time.Parse(time.RFC3339, startTimeStr)

	var endTime time.Time
	if endTimeStr := getString(meta, "end_time"); endTimeStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endTimeStr)
	}
	if updatedAtStr := getString(meta, "updated_at"); updatedAtStr != "" {
		if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			if endTime.IsZero() || t.After(endTime) {
				endTime = t
			}
		}
	}

	var tokenStats *event.TokenStats
	if usageData, ok := meta["token_usage"]; ok {
		if usageMap, ok := usageData.(map[string]interface{}); ok {
			if b, err := json.Marshal(usageMap); err == nil {
				_ = json.Unmarshal(b, &tokenStats)
			}
		}
	}

	task := &ActiveTask{
		ID:          runID,
		SessionID:   sessionID,
		Command:     cmd,
		Title:       title,
		Type:        "unified",
		StartTime:   startTime,
		EndTime:     endTime,
		Status:      status,
		TokenUsage:  tokenStats,
		Store:       NewTaskEventStore(5000),
		Broadcaster: NewEventBroadcaster(),
	}

	return task, nil
}

func (ds *Server) streamHandler(w http.ResponseWriter, r *http.Request) {
	if !ds.authenticateWebSocketRequest(w, r) {
		return
	}

	conn, err := ds.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}
	defer func() {
		logger.Infof("Closing WebSocket connection from %s", r.RemoteAddr)
		_ = conn.Close()
	}()

	logger.Infof("WebSocket connection established from %s", r.RemoteAddr)

	conn.EnableWriteCompression(false)
	conn.SetReadDeadline(time.Now().Add(300 * time.Second)) //nolint:errcheck
	conn.SetWriteDeadline(time.Time{})                      //nolint:errcheck

	connManager := NewConnectionManager(conn)
	defer func() { connManager.Close() }()

	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()

	for {
		if !connManager.IsConnectionAlive() {
			logger.Infof("Connection is no longer alive, stopping message processing")
			break
		}
		var request struct {
			Command   string                `json:"command,omitempty"`
			SessionID string                `json:"session_id,omitempty"`
			RunID     string                `json:"run_id,omitempty"`
			Timeout   int                   `json:"timeout,omitempty"`
			Images    []llm.MultimodalImage `json:"images,omitempty"`
			Type      string                `json:"type,omitempty"`
			SinceSeq  int64                 `json:"since_seq,omitempty"`
			Streams   []string              `json:"streams,omitempty"`
			CallID    string                `json:"call_id,omitempty"`
			Approved  bool                  `json:"approved,omitempty"`
		}

		conn.SetReadDeadline(time.Now().Add(300 * time.Second)) //nolint:errcheck

		if err := conn.ReadJSON(&request); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				logger.Errorf("Unexpected WebSocket close error: %v", err)
			} else {
				logger.Infof("WebSocket connection closed: %v", err)
			}
			break
		}

		// Handle ping messages
		if request.Type == "ping" {
			_ = connManager.SafeWriteJSON(map[string]string{"type": "pong"})
			continue
		}
		if request.Type == "tool_approval" {
			callID := strings.TrimSpace(request.CallID)
			if callID == "" {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":     "error",
					"error":    "call_id is required",
					"severity": "warning",
				})
				continue
			}
			if err := ds.SubmitToolApproval(callID, request.Approved); err != nil {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":    "error",
					"call_id": callID,
					"error":   err.Error(),
				})
				continue
			}
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":     "tool_approval_ack",
				"call_id":  callID,
				"approved": request.Approved,
			})
			continue
		}

		sessionID := strings.TrimSpace(request.SessionID)
		if sessionID == "" {
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":     "error",
				"error":    "session_id is required",
				"severity": "warning",
			})
			continue
		}

		ds.tasksMutex.RLock()
		foundTask := ds.activeTasks[sessionID]
		ds.tasksMutex.RUnlock()

		// If task not found in memory, try to load from disk
		if foundTask == nil {
			if task, err := ds.loadTaskFromDisk(sessionID); err == nil {
				foundTask = task
				ds.tasksMutex.Lock()
				ds.activeTasks[sessionID] = foundTask
				ds.tasksMutex.Unlock()
				// Schedule cleanup so it doesn't stay forever
				ds.scheduleTaskCleanup(sessionID, 5*time.Minute)
			}
		}

		if request.Type == "subscribe" {
			if foundTask == nil {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"error":      "no task found for session",
					"session_id": sessionID,
					"severity":   "warning",
				})
				continue
			}

			// Load history from disk if needed
			if foundTask.Store == nil || foundTask.Store.LastSeq() == 0 {
				if err := foundTask.loadHistoryFromDisk(); err != nil {
					logger.Warnf("Failed to load history for session %s: %v", sessionID, err)
				}
			}

			if request.RunID != "" && request.RunID != foundTask.ID {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"error":      "run_id mismatch",
					"session_id": sessionID,
					"run_id":     request.RunID,
					"severity":   "warning",
				})
				continue
			}
			logger.Infof("Subscribing to task %s in session %s since_seq=%d", foundTask.ID, sessionID, request.SinceSeq)
			if err := ds.streamTaskEventsResumable(connCtx, foundTask, connManager, request.SinceSeq, request.Streams); err != nil {
				logger.Warnf("Stream interrupted: %v", err)
				break
			}
			ds.sendCompletionMessage(foundTask, connManager)
			continue
		}

		if foundTask != nil && foundTask.Status == "running" {
			if strings.TrimSpace(request.Command) != "" {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":            "error",
					"error":           "session is already running; command ignored",
					"severity":        "warning",
					"session_id":      sessionID,
					"ignored_command": true,
				})
			}
			logger.Infof("Attaching to running task %s in session %s", foundTask.ID, sessionID)
			if err := ds.streamTaskEvents(connCtx, foundTask, connManager); err != nil {
				logger.Warnf("Stream interrupted: %v", err)
				break
			}
			ds.sendCompletionMessage(foundTask, connManager)
			continue
		}

		if strings.TrimSpace(request.Command) != "" {
			if ds.isDraining() {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"error":      "daemon is draining and not accepting new executions",
					"session_id": sessionID,
				})
				continue
			}
			logger.Infof("Processing unified command: %s (session: %s)", request.Command, sessionID)

			task, err := ds.startUnifiedTask(request.Command, sessionID, request.Timeout, request.Images)
			if err != nil {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"error":      err.Error(),
					"session_id": sessionID,
				})
				continue
			}

			if err := ds.streamTaskEvents(connCtx, task, connManager); err != nil {
				logger.Warnf("Stream interrupted: %v", err)
				break
			}
			ds.sendCompletionMessage(task, connManager)
			continue
		}

		status := ""
		title := ""
		updatedAt := ""
		if foundTask != nil {
			status = foundTask.Status
			title = foundTask.Title
		}
		if status == "" || title == "" {
			metaPath := filepath.Join(getRuntimeSessionsDir(), sessionID, "metadata.json")
			if data, err := os.ReadFile(metaPath); err == nil {
				var meta map[string]interface{}
				if json.Unmarshal(data, &meta) == nil {
					if status == "" {
						if s, ok := meta["status"].(string); ok {
							status = s
						}
					}
					if title == "" {
						if t, ok := meta["title"].(string); ok {
							title = t
						}
					}
					if ua, ok := meta["updated_at"].(string); ok {
						updatedAt = ua
					}
				}
			}
		}

		_ = connManager.SafeWriteJSON(map[string]interface{}{
			"type":       "status",
			"session_id": sessionID,
			"status":     status,
			"title":      title,
			"updated_at": updatedAt,
		})
	}
}

func (ds *Server) authenticateWebSocketRequest(w http.ResponseWriter, r *http.Request) bool {
	if ds.config.APIKey == "" {
		return true
	}

	query := r.URL.Query()
	var apiKey string
	for _, name := range []string{"api_key", "apikey", "apiKey", "key"} {
		if v := strings.TrimSpace(query.Get(name)); v != "" {
			apiKey = v
			break
		}
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		low := strings.ToLower(auth)
		if strings.HasPrefix(low, "bearer ") {
			apiKey = strings.TrimSpace(auth[7:])
		} else if strings.HasPrefix(low, "apikey ") {
			apiKey = strings.TrimSpace(auth[7:])
		}
	}

	expected := strings.TrimSpace(ds.config.APIKey)
	provided := strings.TrimSpace(apiKey)
	if provided != expected {
		logger.Warnf("WS unauthorized: provided_len=%d expected_len=%d", len(provided), len(expected))
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (ds *Server) sendCompletionMessage(task *ActiveTask, connManager *ConnectionManager) {
	ds.tasksMutex.RLock()
	defer ds.tasksMutex.RUnlock()

	lastSeq := int64(0)
	if task.Store != nil {
		lastSeq = task.Store.LastSeq()
	}
	completionData := map[string]interface{}{
		"type":         "completion",
		"session_id":   task.SessionID,
		"run_id":       task.ID,
		"success":      task.Status == "completed",
		"token_stats":  task.TokenUsage,
		"status":       task.Status,
		"session_done": task.Status == "completed" || task.Status == "cancelled" || task.Status == "error" || task.Status == "timeout",
		"last_seq":     lastSeq,
	}

	_ = connManager.SafeWriteJSON(completionData)
}

// scheduleTaskCleanup removes finished tasks from activeTasks after a grace delay
func (ds *Server) scheduleTaskCleanup(sessionID string, delay time.Duration) {
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		ds.tasksMutex.Lock()
		if t, ok := ds.activeTasks[sessionID]; ok && t != nil && t.Status != "running" {
			delete(ds.activeTasks, sessionID)
		}
		ds.tasksMutex.Unlock()
	}()
}
func (ds *Server) mcpStatusHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get actual MCP status from agent's toolbox
	var status map[string]interface{}
	if ds.agent != nil && ds.agent.GetToolbox() != nil {
		toolbox := ds.agent.GetToolbox()
		mcpStatus := toolbox.GetMCPStatus()
		mcpConnections := toolbox.ListMCPConnections()

		// Calculate total tools (MCP + Internal) to match mcpToolsHandler list
		totalTools := 0
		if val, ok := mcpStatus["available_tools"].(int); ok {
			totalTools += val
		}
		// Add internal tools count - DISABLED
		/*
			if internalTools := toolbox.List(); internalTools != nil {
				totalTools += len(internalTools)
			}
		*/

		status = map[string]interface{}{
			"enabled":     toolbox.IsMCPEnabled(),
			"servers":     mcpStatus["configured_servers"],
			"tools":       totalTools, // Use total count to match /mcp/tools list
			"connections": mcpConnections,
			"resources":   mcpStatus["available_resources"],
			"prompts":     mcpStatus["available_prompts"],
		}
	} else {
		// Fallback if agent or toolbox is not available
		status = map[string]interface{}{
			"enabled":     false,
			"servers":     0,
			"tools":       0,
			"connections": []interface{}{},
			"resources":   0,
			"prompts":     0,
		}
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Errorf("Failed to encode MCP status response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (ds *Server) mcpToolsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get actual MCP tools from agent's toolbox
	var tools map[string]interface{}
	if ds.agent != nil && ds.agent.GetToolbox() != nil {
		toolbox := ds.agent.GetToolbox()
		mcpConnections := toolbox.ListMCPConnections()

		// Convert MCP connections to tools list format
		toolsList := make([]interface{}, 0)
		for _, conn := range mcpConnections {
			// Get tools from MCP client for this connection
			if mcpClient := toolbox.GetMCPClient(); mcpClient != nil {
				allTools := mcpClient.GetAllTools()
				if serverTools, exists := allTools[conn.Name]; exists {
					for _, tool := range serverTools {
						toolsList = append(toolsList, map[string]interface{}{
							"name":        tool.Name,
							"description": tool.Description,
							"server":      conn.Name,
							"connected":   conn.Connected,
						})
					}
				}
			}
		}

		// Also include internal tools - DISABLED
		/*
			internalTools := toolbox.List()
			for _, tool := range internalTools {
				toolsList = append(toolsList, map[string]interface{}{
					"name":        tool.Name(),
					"description": tool.Description(),
					"server":      "nano-agent",
					"connected":   true,
				})
			}
		*/

		tools = map[string]interface{}{
			"tools": toolsList,
		}
	} else {
		// Fallback if agent or toolbox is not available
		tools = map[string]interface{}{
			"tools": []interface{}{},
		}
	}

	if err := json.NewEncoder(w).Encode(tools); err != nil {
		logger.Errorf("Failed to encode MCP tools response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (ds *Server) mcpDiagnosticsHandler(w http.ResponseWriter, _ *http.Request) {
	// This would get diagnostics from MCP client
	response := map[string]interface{}{
		"status":  "healthy",
		"servers": []interface{}{},
		"metrics": map[string]interface{}{},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ds *Server) memoryHandler(w http.ResponseWriter, r *http.Request) {
	if ds.agent == nil {
		http.Error(w, "Agent not initialized", http.StatusInternalServerError)
		return
	}

	memoryManager := ds.agent.GetMemoryManager()
	if memoryManager == nil {
		http.Error(w, "Memory manager not available", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case "GET":
		// Actually retrieve memory entries
		ctx := context.Background()
		result, err := memoryManager.SearchMemory(ctx, "", "", "", 100)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve memory entries: %v", err), http.StatusInternalServerError)
			return
		}

		var entries []interface{}
		count := 0

		if result != "" {
			// Handle the search result data
			entries = append(entries, map[string]interface{}{
				"message": result,
			})
			count = 1
		}

		response := map[string]interface{}{
			"entries": entries,
			"count":   count,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)

	case "POST":
		// Actually save memory entry
		var request struct {
			Key      string   `json:"key"`
			Content  string   `json:"content"`
			Category string   `json:"category,omitempty"`
			Tags     []string `json:"tags,omitempty"`
			Priority string   `json:"priority,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Set defaults
		if request.Category == "" {
			request.Category = "General"
		}
		if request.Priority == "" {
			request.Priority = "medium"
		}

		ctx := context.Background()
		metadata := map[string]interface{}{
			"category": request.Category,
			"tags":     strings.Join(request.Tags, ","),
			"priority": request.Priority,
		}

		// Convert string content directly to mem0 message format
		messages := []llm.Message{
			{
				Role:    "user",
				Content: request.Content,
			},
		}

		err := memoryManager.SaveMemory(ctx, messages, request.Category, "", metadata)

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save memory entry: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"success": true,
			"key":     request.Key,
			"message": "Memory entry saved successfully",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

func (ds *Server) memoryItemHandler(w http.ResponseWriter, r *http.Request) {
	if ds.agent == nil {
		http.Error(w, "Agent not initialized", http.StatusInternalServerError)
		return
	}

	memoryManager := ds.agent.GetMemoryManager()
	if memoryManager == nil {
		http.Error(w, "Memory manager not available", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	key := vars["key"]

	switch r.Method {
	case "GET":
		// Get memory entry by searching for the key
		ctx := context.Background()
		result, err := memoryManager.SearchMemory(ctx, key, "", "", 10)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve memory entry: %v", err), http.StatusInternalServerError)
			return
		}

		var found bool
		var content string

		if result != "" {
			found = true
			content = result
		}

		response := map[string]interface{}{
			"key":     key,
			"content": content,
			"found":   found,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)

	case "DELETE":
		// Note: Current Memory Manager doesn't have a direct delete by ID method
		response := map[string]interface{}{
			"success": false,
			"key":     key,
			"error":   "Delete operation not yet implemented in Memory Manager",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(response)
	}
}

// Utility functions
func (ds *Server) createPidFile() error {
	pidDir := filepath.Dir(ds.config.PidFile)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return err
	}

	pid := os.Getpid()
	return os.WriteFile(ds.config.PidFile, []byte(strconv.Itoa(pid)), 0644)
}

func (ds *Server) removePidFile() {
	_ = os.Remove(ds.config.PidFile)
}

// Monitoring handlers
func (ds *Server) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	sysMetrics, perfMetrics := ds.systemMonitor.GetCurrentMetrics()

	response := map[string]interface{}{
		"system":      sysMetrics,
		"performance": perfMetrics,
		"timestamp":   time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// Session management handlers

func (ds *Server) metricsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100 // default limit
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	sysHistory, perfHistory := ds.systemMonitor.GetMetricsHistory(limit)

	response := map[string]interface{}{
		"system_history":      sysHistory,
		"performance_history": perfHistory,
		"count":               len(sysHistory),
		"timestamp":           time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ds *Server) systemHealthHandler(w http.ResponseWriter, _ *http.Request) {
	healthStatus := ds.systemMonitor.GetHealthStatus()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthStatus)
}

// IsRunning checks if daemon is already running
func IsRunning(pidFile string) bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Try to signal the process
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
func (ds *Server) commandsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// When no agent is running (or no working directory is known), return only
	// built-in commands to avoid unexpected filesystem access in the daemon
	// process user's home directory.
	var list []slash.Command
	if ds.agent != nil && ds.agent.GetToolbox() != nil {
		cwd := ds.agent.GetToolbox().GetWorkingDirectory()
		if cwd != "" {
			list = slash.NewRegistry(cwd).All()
		}
	}
	if list == nil {
		list = slash.NewBuiltinRegistry().All()
	}

	out := make([]map[string]any, 0, len(list))
	for _, cmd := range list {
		entry := map[string]any{
			"name":        cmd.Name,
			"description": cmd.Description,
			"usage":       cmd.Usage,
			"category":    string(cmd.Category),
			"source":      cmd.Source,
		}
		if cmd.Namespace != "" {
			entry["namespace"] = cmd.Namespace
		}
		if len(cmd.AllowedTools) > 0 {
			entry["allowedTools"] = cmd.AllowedTools
		}
		out = append(out, entry)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"commands": out})
}

// Team-lead session handlers

// CreateTeamLeadSessionRequest represents a request to create a team-lead session
type CreateTeamLeadSessionRequest struct {
	SessionID          string `json:"session_id,omitempty"`
	TeamName           string `json:"team_name"`
	InteractiveConfirm bool   `json:"interactive_confirm,omitempty"`
}

// TeamLeadSessionResponse represents a team-lead session response
type TeamLeadSessionResponse struct {
	SessionID    string    `json:"session_id"`
	TeamName     string    `json:"team_name"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

// createTeamLeadSessionHandler creates a new team-lead session
func (ds *Server) createTeamLeadSessionHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	var req CreateTeamLeadSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.TeamName == "" {
		req.TeamName = "default"
	}

	// Generate session ID if not provided
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("lead-%d-%s", time.Now().Unix(), generateRandomID())
	}

	// Get agent config
	cfg := ds.agent.GetConfig()

	// Create approval handler (auto-approve for daemon)
	approvalHandler := func(*agent.ToolCallInfo) bool {
		return true
	}

	// Create or get session
	session, err := ds.teamLeadRegistry.GetOrCreate(req.SessionID, req.TeamName, cfg, approvalHandler)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}
	if req.InteractiveConfirm {
		session.EnableInteractiveApproval()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TeamLeadSessionResponse{
		SessionID:    session.ID,
		TeamName:     session.TeamName,
		CreatedAt:    session.CreatedAt,
		LastActiveAt: session.LastActiveAt,
	})
}

// listTeamLeadSessionsHandler lists all active team-lead sessions
func (ds *Server) listTeamLeadSessionsHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	sessions := ds.teamLeadRegistry.List()
	responses := make([]TeamLeadSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		responses = append(responses, TeamLeadSessionResponse{
			SessionID:    session.ID,
			TeamName:     session.TeamName,
			CreatedAt:    session.CreatedAt,
			LastActiveAt: session.LastActiveAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": responses,
		"count":    len(responses),
	})
}

// getTeamLeadSessionHandler gets details of a specific team-lead session
func (ds *Server) getTeamLeadSessionHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["id"]

	session, exists := ds.teamLeadRegistry.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TeamLeadSessionResponse{
		SessionID:    session.ID,
		TeamName:     session.TeamName,
		CreatedAt:    session.CreatedAt,
		LastActiveAt: session.LastActiveAt,
	})
}

// deleteTeamLeadSessionHandler deletes a team-lead session
func (ds *Server) deleteTeamLeadSessionHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["id"]

	if err := ds.teamLeadRegistry.Remove(sessionID); err != nil {
		if errors.Is(err, ErrTeamLeadSessionNotFound) {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Session deleted successfully",
	})
}

// ExecuteInTeamLeadSessionRequest represents a request to execute in a team-lead session
type ExecuteInTeamLeadSessionRequest struct {
	Command string `json:"command"`
}

// executeInTeamLeadSessionHandler executes a command in a team-lead session
func (ds *Server) executeInTeamLeadSessionHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["id"]

	session, exists := ds.teamLeadRegistry.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	var req ExecuteInTeamLeadSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Command == "" {
		http.Error(w, "Command is required", http.StatusBadRequest)
		return
	}

	// Generate task ID
	taskID := generateTaskID()

	// Create a response channel to collect events
	events := make([]event.StreamEvent, 0)
	var mu sync.Mutex

	callback := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	// Execute the command
	ctx := r.Context()
	err := session.Execute(ctx, taskID, req.Command, callback)

	mu.Lock()
	defer mu.Unlock()

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"events":  events,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"events":  events,
	})
}

// cancelTeamLeadSessionHandler cancels all currently running turns in a team-lead session.
func (ds *Server) cancelTeamLeadSessionHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := mux.Vars(r)["id"]
	session, exists := ds.teamLeadRegistry.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	cancelled := session.CancelActiveTasks()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"cancelled_tasks": cancelled,
	})
}

// teamLeadSessionEventsHandler returns resumable team-lead events since a sequence number.
func (ds *Server) teamLeadSessionEventsHandler(w http.ResponseWriter, r *http.Request) {
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := mux.Vars(r)["id"]
	session, exists := ds.teamLeadRegistry.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	sinceSeq := int64(0)
	if raw := r.URL.Query().Get("since_seq"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "Invalid since_seq", http.StatusBadRequest)
			return
		}
		sinceSeq = parsed
	}

	events := []event.StreamEvent{}
	lastSeq := int64(0)
	if session.Store != nil {
		lastSeq = session.Store.LastSeq()
		events = session.Store.Since(sinceSeq, func(event.StreamEvent) bool { return true })
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": session.ID,
		"since_seq":  sinceSeq,
		"last_seq":   lastSeq,
		"events":     events,
	})
}

func (ds *Server) teamLeadSessionStreamHandler(w http.ResponseWriter, r *http.Request) {
	if !ds.authenticateWebSocketRequest(w, r) {
		return
	}
	if ds.teamLeadRegistry == nil {
		http.Error(w, "Team-lead sessions not enabled", http.StatusServiceUnavailable)
		return
	}

	sessionID := mux.Vars(r)["id"]
	session, exists := ds.teamLeadRegistry.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	conn, err := ds.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade team-lead stream: %v", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	conn.EnableWriteCompression(false)
	_ = conn.SetReadDeadline(time.Now().Add(teamLeadStreamReadTimeout))
	_ = conn.SetWriteDeadline(time.Time{})

	connManager := NewConnectionManager(conn)
	defer connManager.Close()

	for connManager.IsConnectionAlive() {
		var req struct {
			Type     string `json:"type"`
			Command  string `json:"command,omitempty"`
			TaskID   string `json:"task_id,omitempty"`
			SinceSeq int64  `json:"since_seq,omitempty"`
			CallID   string `json:"call_id,omitempty"`
			Approved bool   `json:"approved,omitempty"`
		}
		_ = conn.SetReadDeadline(time.Now().Add(teamLeadStreamReadTimeout))
		if err := conn.ReadJSON(&req); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				logger.Errorf("Unexpected team-lead WebSocket close error: %v", err)
			}
			break
		}

		switch strings.TrimSpace(req.Type) {
		case "ping":
			_ = connManager.SafeWriteJSON(map[string]string{"type": "pong"})
		case "subscribe":
			ds.streamTeamLeadReplayAndLive(session, connManager, req.SinceSeq)
		case "cancel":
			cancelled := session.CancelActiveTasks()
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":            "cancel_ack",
				"session_id":      session.ID,
				"cancelled_tasks": cancelled,
			})
		case "tool_approval":
			callID := strings.TrimSpace(req.CallID)
			if callID == "" {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"session_id": session.ID,
					"error":      "call_id is required",
					"severity":   "warning",
				})
				continue
			}
			if err := session.SubmitToolApproval(callID, req.Approved); err != nil {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"session_id": session.ID,
					"call_id":    callID,
					"error":      err.Error(),
				})
				continue
			}
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":       "tool_approval_ack",
				"session_id": session.ID,
				"call_id":    callID,
				"approved":   req.Approved,
			})
		case "lead_input":
			if strings.TrimSpace(req.Command) == "" {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"session_id": session.ID,
					"error":      "command is required",
					"severity":   "warning",
				})
				continue
			}
			if taskID, ok := teamLeadRunningTask(session); ok {
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"session_id": session.ID,
					"task_id":    taskID,
					"error":      "team-lead session is already running; input ignored",
					"severity":   "warning",
				})
				continue
			}
			taskID := strings.TrimSpace(req.TaskID)
			if taskID == "" {
				taskID = generateTaskID()
			}
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":       "lead_input_ack",
				"session_id": session.ID,
				"team_name":  session.TeamName,
				"task_id":    taskID,
			})
			status := "completed"
			err := session.Execute(context.Background(), taskID, req.Command, func(e event.StreamEvent) {
				if e.Seq == 0 {
					e = session.enrichAndRecordEvent(e)
				}
				_ = connManager.SafeWriteJSON(e)
			})
			if err != nil {
				status = "error"
				_ = connManager.SafeWriteJSON(map[string]interface{}{
					"type":       "error",
					"session_id": session.ID,
					"task_id":    taskID,
					"error":      err.Error(),
				})
			}
			ds.sendTeamLeadCompletion(session, taskID, status, err == nil, connManager)
		default:
			_ = connManager.SafeWriteJSON(map[string]interface{}{
				"type":       "error",
				"session_id": session.ID,
				"error":      "unsupported team-lead stream frame type",
				"severity":   "warning",
			})
		}
	}
}

func (ds *Server) streamTeamLeadReplayAndLive(session *TeamLeadSession, connManager *ConnectionManager, sinceSeq int64) {
	_ = connManager.SafeWriteJSON(map[string]interface{}{
		"type":       "session_start",
		"session_id": session.ID,
		"team_name":  session.TeamName,
	})
	if session.Store != nil {
		for _, e := range session.Store.Since(sinceSeq, func(event.StreamEvent) bool { return true }) {
			_ = connManager.SafeWriteJSON(e)
		}
	}

	taskID, running := teamLeadRunningTask(session)
	if !running {
		ds.sendTeamLeadCompletion(session, "", "idle", true, connManager)
		return
	}
	sub := session.Broadcaster.Subscribe()
	defer session.Broadcaster.Unsubscribe(sub)
	for {
		select {
		case e := <-sub:
			if e.Seq <= sinceSeq {
				continue
			}
			_ = connManager.SafeWriteJSON(e)
			if e.Type == event.EventTypeTaskCompletion {
				ds.sendTeamLeadCompletion(session, taskID, "completed", true, connManager)
				return
			}
		case <-time.After(30 * time.Second):
			if _, stillRunning := teamLeadRunningTask(session); !stillRunning {
				ds.sendTeamLeadCompletion(session, taskID, "completed", true, connManager)
				return
			}
		}
	}
}

func (ds *Server) sendTeamLeadCompletion(session *TeamLeadSession, taskID, status string, success bool, connManager *ConnectionManager) {
	lastSeq := int64(0)
	if session.Store != nil {
		lastSeq = session.Store.LastSeq()
	}
	_ = connManager.SafeWriteJSON(map[string]interface{}{
		"type":         "completion",
		"session_id":   session.ID,
		"team_name":    session.TeamName,
		"task_id":      taskID,
		"success":      success,
		"status":       status,
		"session_done": status != "running",
		"last_seq":     lastSeq,
	})
}

func teamLeadRunningTask(session *TeamLeadSession) (string, bool) {
	session.tasksMutex.RLock()
	defer session.tasksMutex.RUnlock()
	for _, task := range session.activeTasks {
		if task != nil && task.Status == "running" {
			return task.ID, true
		}
	}
	return "", false
}

// generateRandomID generates a random ID for sessions and tasks
func generateRandomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateTaskID() string {
	return fmt.Sprintf("task-%d-%s", time.Now().Unix(), generateRandomID())
}
