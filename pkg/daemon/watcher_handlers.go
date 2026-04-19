package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/nano-harness/nano-agent/pkg/watcher"
	"github.com/gorilla/mux"
)

// watcherListRulesHandler handles GET /watcher/rules.
func (ds *Server) watcherListRulesHandler(w http.ResponseWriter, r *http.Request) {
	if ds.watcher == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "watcher is not enabled; set watcher.enabled: true in config",
		})
		return
	}

	rules := ds.watcher.ListRules()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rules": rules})
}

// watcherAddRuleHandler handles POST /watcher/rules.
// Expects a JSON body matching watcher.Rule (ID is optional, auto-generated).
func (ds *Server) watcherAddRuleHandler(w http.ResponseWriter, r *http.Request) {
	if ds.watcher == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "watcher is not enabled; set watcher.enabled: true in config",
		})
		return
	}

	var rule watcher.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body: " + err.Error()})
		return
	}

	if rule.Source == "" || rule.Command == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "source and command are required"})
		return
	}

	// Validate source-specific required fields.
	if rule.Source == "shell" && rule.ShellCommand == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "shell_command is required when source is 'shell'",
		})
		return
	}

	rule = ds.watcher.AddRule(rule)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(rule)
}

// watcherDeleteRuleHandler handles DELETE /watcher/rules/{id}.
func (ds *Server) watcherDeleteRuleHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if ds.watcher == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "watcher is not enabled"})
		return
	}

	if err := ds.watcher.RemoveRule(id); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
