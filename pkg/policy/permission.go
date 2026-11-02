package policy

// PermissionAction is the result of a permission decision.
type PermissionAction int

const (
	PermissionAllow   PermissionAction = iota // Proceed automatically (read-only, safe commands)
	PermissionConfirm                         // Request user confirmation before proceeding
	PermissionBlock                           // Hard-block, do not execute
)

func (a PermissionAction) String() string {
	switch a {
	case PermissionAllow:
		return "allow"
	case PermissionConfirm:
		return "confirm"
	case PermissionBlock:
		return "block"
	default:
		return "unknown"
	}
}

// PermissionDecision is the result of evaluating a permission check (config rules, hooks, analyzers).
type PermissionDecision struct {
	Action         PermissionAction       `json:"action"`
	Reason         string                 `json:"reason,omitempty"`
	Rule           string                 `json:"rule,omitempty"`
	Layer          int                    `json:"layer,omitempty"` // Layer that produced this decision (1-4)
	Warnings       []string               `json:"warnings,omitempty"`
	Confidence     float64                `json:"confidence,omitempty"`
	Suggestions    []string               `json:"suggestions,omitempty"`
	AutoWhitelist  bool                   `json:"auto_whitelist,omitempty"`
	ModifiedParams map[string]interface{} `json:"modified_params,omitempty"`
	AuditMetadata  map[string]interface{} `json:"audit_metadata,omitempty"`
}

// Layer constants identify which decision layer produced the result.
const (
	LayerConfig   = 1
	LayerHook     = 2
	LayerAnalyzer = 3
)
