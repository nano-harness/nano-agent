package tools

// ToolConfigMap converts typed ToolboxConfig into the legacy map consumed by
// existing tool constructors. Keeping this adapter centralized lets callers
// migrate toward typed config without changing every tool constructor at once.
func (c *ToolboxConfig) ToolConfigMap() map[string]interface{} {
	config := make(map[string]interface{})
	if c == nil {
		return config
	}

	if len(c.AllowedCommands) > 0 {
		config["allowed_commands"] = c.AllowedCommands
	}
	if len(c.BlockedCommands) > 0 {
		config["blocked_commands"] = c.BlockedCommands
	}
	config["max_file_size"] = c.MaxFileSize
	config["max_response_size"] = c.MaxResponseSize
	config["timeout"] = c.Timeout
	config["user_agent"] = c.UserAgent

	config["read_file_max_lines"] = c.ReadFileMaxLines
	config["search_max_results"] = c.SearchMaxResults
	config["web_request_timeout"] = c.WebRequestTimeout
	config["web_search_timeout"] = c.WebSearchTimeout
	config["web_max_content_size"] = c.WebMaxContentSize
	config["web_search_max_results"] = c.WebSearchMaxResults
	config["file_diff_max_lines"] = c.FileDiffMaxLines
	config["git_max_log_entries"] = c.GitMaxLogEntries

	config["allowed_env_vars"] = c.AllowedEnvVars
	config["blocked_env_vars"] = c.BlockedEnvVars
	config["strict"] = c.Strict

	if c.ImageAPIKey != "" {
		config["image_api_key"] = c.ImageAPIKey
	}
	if c.ImageBaseURL != "" {
		config["image_base_url"] = c.ImageBaseURL
	}

	return config
}
