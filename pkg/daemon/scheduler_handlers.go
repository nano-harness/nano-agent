package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type scheduleTaskRequest struct {
	CronExpression string `json:"cron_expression"`
	Command        string `json:"command"`
}

func (ds *Server) scheduleTaskHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if ds.scheduler == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "scheduler is not enabled",
		})
		return
	}

	var req scheduleTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "invalid request format",
		})
		return
	}

	if req.CronExpression == "" || req.Command == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "cron_expression and command are required",
		})
		return
	}

	task, err := ds.scheduler.ScheduleTask(req.CronExpression, req.Command)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"task":    task,
	})
}

func (ds *Server) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if ds.scheduler == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "scheduler is not enabled",
		})
		return
	}

	tasks := ds.scheduler.ListTasks()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"tasks":   tasks,
	})
}

func (ds *Server) deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if ds.scheduler == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "scheduler is not enabled",
		})
		return
	}

	vars := mux.Vars(r)
	taskID := vars["id"]

	err := ds.scheduler.RemoveTask(taskID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
	})
}
