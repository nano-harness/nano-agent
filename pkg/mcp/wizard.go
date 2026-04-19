package mcp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"gopkg.in/yaml.v2"
)

// ConfigWizard provides an interactive configuration wizard for MCP
type ConfigWizard struct {
	config *MCPConfig
	reader *bufio.Reader
}

// NewConfigWizard creates a new configuration wizard
func NewConfigWizard() *ConfigWizard {
	return &ConfigWizard{
		config: createDefaultMCPConfig(),
		reader: bufio.NewReader(os.Stdin),
	}
}

// createDefaultMCPConfig creates a default MCP configuration
func createDefaultMCPConfig() *MCPConfig {
	return &MCPConfig{
		EnableClient:     true,
		MCPServers:       []MCPServerConfig{},
		DefaultTransport: "stdio",
		Timeout:          30 * time.Second,
		MaxRetries:       3,
		EnableAuth:       false,
		AuthTokens:       make(map[string]string),
		TLSConfig: &TLSConfig{
			Enabled:    false,
			SkipVerify: false,
		},
	}
}

// Run starts the interactive configuration wizard
func (cw *ConfigWizard) Run() (*MCPConfig, error) {
	fmt.Println("=== nano-agent MCP Configuration Wizard ===")
	fmt.Println("This wizard will help you set up Model Context Protocol integration.")
	fmt.Println()

	// Step 1: Enable MCP
	if err := cw.askEnableMCP(); err != nil {
		return nil, err
	}

	if !cw.config.EnableClient {
		fmt.Println("MCP disabled. Configuration complete.")
		return cw.config, nil
	}

	// Step 2: Basic configuration
	if err := cw.askBasicConfiguration(); err != nil {
		return nil, err
	}

	// Step 3: Add servers
	if err := cw.askAddServers(); err != nil {
		return nil, err
	}

	// Step 4: Advanced settings
	if err := cw.askAdvancedSettings(); err != nil {
		return nil, err
	}

	// Step 5: Save configuration
	if err := cw.askSaveConfiguration(); err != nil {
		return nil, err
	}

	fmt.Println("\n✓ MCP configuration completed successfully!")
	return cw.config, nil
}

// askEnableMCP asks whether to enable MCP
func (cw *ConfigWizard) askEnableMCP() error {
	fmt.Print("Do you want to enable MCP client functionality? [Y/n]: ")
	response, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	cw.config.EnableClient = response == "" || response == "y" || response == "yes"

	if cw.config.EnableClient {
		fmt.Println("✓ MCP client enabled")
	} else {
		fmt.Println("✗ MCP client disabled")
	}

	return nil
}

// askBasicConfiguration asks for basic configuration
func (cw *ConfigWizard) askBasicConfiguration() error {
	fmt.Println("\n--- Basic Configuration ---")

	// Default transport
	fmt.Print("Default transport type [stdio/streamable/inmemory] (stdio): ")
	transport, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	transport = strings.TrimSpace(transport)
	if transport == "" {
		transport = "stdio"
	}
	cw.config.DefaultTransport = transport

	// Timeout
	fmt.Print("Connection timeout in seconds (30): ")
	timeoutStr, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	timeoutStr = strings.TrimSpace(timeoutStr)
	if timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			logger.Warnf("Invalid timeout, using default (30s)")
		} else {
			cw.config.Timeout = time.Duration(timeout) * time.Second
		}
	}

	// Max retries
	fmt.Print("Maximum retry attempts (3): ")
	retriesStr, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	retriesStr = strings.TrimSpace(retriesStr)
	if retriesStr != "" {
		retries, err := strconv.Atoi(retriesStr)
		if err != nil {
			logger.Warnf("Invalid retry count, using default (3)")
		} else {
			cw.config.MaxRetries = retries
		}
	}

	return nil
}

// askAddServers asks about adding MCP servers
func (cw *ConfigWizard) askAddServers() error {
	fmt.Println("\n--- Server Configuration ---")
	fmt.Print("Do you want to add MCP servers? [Y/n]: ")

	response, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "n" || response == "no" {
		return nil
	}

	// Show predefined servers
	predefined := cw.getPredefinedServers()
	if len(predefined) > 0 {
		fmt.Println("\nAvailable predefined servers:")
		for i, server := range predefined {
			logger.Infof("  %d. %s - %s", i+1, server.Name, server.Description)
		}

		fmt.Print("\nAdd predefined servers? Enter numbers separated by commas (e.g., 1,3,5) or 'skip': ")
		selection, err := cw.reader.ReadString('\n')
		if err != nil {
			return err
		}

		selection = strings.TrimSpace(selection)
		if selection != "skip" && selection != "" {
			if err := cw.addPredefinedServers(selection, predefined); err != nil {
				logger.Errorf("Error adding predefined servers: %v", err)
			}
		}
	}

	// Ask for custom servers
	for {
		fmt.Print("\nDo you want to add a custom server? [y/N]: ")
		response, err := cw.reader.ReadString('\n')
		if err != nil {
			return err
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			break
		}

		if err := cw.addCustomServer(); err != nil {
			logger.Errorf("Error adding custom server: %v", err)
		}
	}

	return nil
}

// addPredefinedServers adds selected predefined servers
func (cw *ConfigWizard) addPredefinedServers(selection string, predefined []MCPServerConfig) error {
	selections := strings.Split(selection, ",")

	for _, sel := range selections {
		sel = strings.TrimSpace(sel)
		index, err := strconv.Atoi(sel)
		if err != nil {
			continue
		}

		if index >= 1 && index <= len(predefined) {
			server := predefined[index-1]
			server.Enabled = true
			cw.config.MCPServers = append(cw.config.MCPServers, server)
			logger.Infof("✓ Added server: %s", server.Name)
		}
	}

	return nil
}

// addCustomServer adds a custom server configuration
func (cw *ConfigWizard) addCustomServer() error {
	var server MCPServerConfig

	// Server name
	fmt.Print("Server name: ")
	name, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	server.Name = strings.TrimSpace(name)

	// Description
	fmt.Print("Description (optional): ")
	desc, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	server.Description = strings.TrimSpace(desc)

	// Transport
	fmt.Printf("Transport type [stdio/streamable] (%s): ", cw.config.DefaultTransport)
	transport, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	transport = strings.TrimSpace(transport)
	if transport == "" {
		transport = cw.config.DefaultTransport
	}
	server.Transport = transport

	// Transport-specific configuration
	switch transport {
	case "stdio":
		fmt.Print("Command (e.g., 'python', 'server.py'): ")
		cmdStr, err := cw.reader.ReadString('\n')
		if err != nil {
			return err
		}
		cmdStr = strings.TrimSpace(cmdStr)
		if cmdStr != "" {
			server.Command = strings.Fields(cmdStr)
		}

	case "streamable":
		fmt.Print("Server URL: ")
		url, err := cw.reader.ReadString('\n')
		if err != nil {
			return err
		}
		server.URL = strings.TrimSpace(url)
	}

	// Set defaults
	server.Enabled = true
	server.Timeout = cw.config.Timeout
	if server.Headers == nil {
		server.Headers = make(map[string]string)
	}

	cw.config.MCPServers = append(cw.config.MCPServers, server)
	logger.Infof("✓ Added custom server: %s", server.Name)

	return nil
}

// askAdvancedSettings asks for advanced configuration
func (cw *ConfigWizard) askAdvancedSettings() error {
	fmt.Print("\nConfigure advanced settings? [y/N]: ")
	response, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return nil
	}

	fmt.Println("\n--- Advanced Settings ---")

	// TLS settings
	fmt.Print("Enable TLS for connections? [y/N]: ")
	tlsResponse, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}
	tlsResponse = strings.TrimSpace(strings.ToLower(tlsResponse))
	if tlsResponse == "y" || tlsResponse == "yes" {
		if cw.config.TLSConfig == nil {
			cw.config.TLSConfig = &TLSConfig{}
		}
		cw.config.TLSConfig.Enabled = true

		fmt.Print("Skip TLS certificate verification (for development)? [y/N]: ")
		skipResponse, err := cw.reader.ReadString('\n')
		if err != nil {
			return err
		}
		skipResponse = strings.TrimSpace(strings.ToLower(skipResponse))
		cw.config.TLSConfig.SkipVerify = skipResponse == "y" || skipResponse == "yes"
	}

	return nil
}

// askSaveConfiguration asks where to save the configuration
func (cw *ConfigWizard) askSaveConfiguration() error {
	fmt.Print("\nSave configuration to file? [Y/n]: ")
	response, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "n" || response == "no" {
		return nil
	}

	// Default path
	cwd, _ := os.Getwd()
	defaultPath := filepath.Join(cwd, ".nano.yaml")

	fmt.Printf("Configuration file path (%s): ", defaultPath)
	pathResponse, err := cw.reader.ReadString('\n')
	if err != nil {
		return err
	}

	configPath := strings.TrimSpace(pathResponse)
	if configPath == "" {
		configPath = defaultPath
	}

	// Save configuration
	return cw.saveConfiguration(configPath)
}

// saveConfiguration saves the configuration to file
func (cw *ConfigWizard) saveConfiguration(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Build minimal top-level config with only MCP-related settings
	out := struct {
		EnableMCP bool              `yaml:"enable_mcp"`
		MCP       *config.MCPConfig `yaml:"mcp"`
	}{
		EnableMCP: cw.config.EnableClient,
		MCP:       convertToConfigMCP(cw.config),
	}

	// Marshal to YAML
	data, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}

	logger.Infof("✓ Configuration saved to: %s", path)
	return nil
}

// convertToConfigMCP converts wizard MCP config to global config format with YAML tags
func convertToConfigMCP(m *MCPConfig) *config.MCPConfig {
	if m == nil {
		return nil
	}

	var tls *config.MCPTLSConfig
	if m.TLSConfig != nil {
		tls = &config.MCPTLSConfig{
			Enabled:    m.TLSConfig.Enabled,
			CertFile:   m.TLSConfig.CertFile,
			KeyFile:    m.TLSConfig.KeyFile,
			CAFile:     m.TLSConfig.CAFile,
			SkipVerify: m.TLSConfig.SkipVerify,
		}
	}

	servers := make([]config.MCPServerConfig, 0, len(m.MCPServers))
	for _, s := range m.MCPServers {
		servers = append(servers, config.MCPServerConfig{
			Name:        s.Name,
			Description: s.Description,
			Command:     s.Command,
			URL:         s.URL,
			Transport:   s.Transport,
			Headers:     s.Headers,
			Enabled:     s.Enabled,
			Timeout:     s.Timeout,
		})
	}

	return &config.MCPConfig{
		EnableClient:     m.EnableClient,
		Servers:          servers,
		DefaultTransport: m.DefaultTransport,
		Timeout:          m.Timeout,
		MaxRetries:       m.MaxRetries,
		EnableAuth:       m.EnableAuth,
		AuthTokens:       m.AuthTokens,
		TLS:              tls,
	}
}

// getPredefinedServers returns a list of predefined server configurations
func (cw *ConfigWizard) getPredefinedServers() []MCPServerConfig {
	defaultTimeout := 30 * time.Second
	return []MCPServerConfig{
		{
			Name:        "filesystem",
			Description: "File system operations (read, write, list files)",
			Transport:   "stdio",
			Command:     []string{"npx", "@modelcontextprotocol/server-filesystem"},
			Enabled:     false,
			Timeout:     defaultTimeout,
			Headers:     make(map[string]string),
		},
		{
			Name:        "git",
			Description: "Git repository operations",
			Transport:   "stdio",
			Command:     []string{"npx", "@modelcontextprotocol/server-git"},
			Enabled:     false,
			Timeout:     defaultTimeout,
			Headers:     make(map[string]string),
		},
		{
			Name:        "brave-search",
			Description: "Web search using Brave Search API",
			Transport:   "stdio",
			Command:     []string{"npx", "@modelcontextprotocol/server-brave-search"},
			Enabled:     false,
			Timeout:     defaultTimeout,
			Headers:     make(map[string]string),
		},
		{
			Name:        "postgres",
			Description: "PostgreSQL database operations",
			Transport:   "stdio",
			Command:     []string{"npx", "@modelcontextprotocol/server-postgres"},
			Enabled:     false,
			Timeout:     defaultTimeout,
			Headers:     make(map[string]string),
		},
		{
			Name:        "slack",
			Description: "Slack workspace integration",
			Transport:   "stdio",
			Command:     []string{"npx", "@modelcontextprotocol/server-slack"},
			Enabled:     false,
			Timeout:     defaultTimeout,
			Headers:     make(map[string]string),
		},
		{
			Name:        "chrome-devtools",
			Description: "Chrome DevTools automation via a headless isolated browser",
			Transport:   "stdio",
			Command: []string{
				"npx",
				"-y",
				"chrome-devtools-mcp@latest",
				"--headless=true",
				"--isolated=true",
				"--viewport=1920x1080",
			},
			Enabled: false,
			Timeout: defaultTimeout,
			Headers: make(map[string]string),
		},
	}
}

// ValidateConfiguration validates a configuration
func ValidateConfiguration(config *MCPConfig) []string {
	var errors []string

	if config == nil {
		errors = append(errors, "Configuration is nil")
		return errors
	}

	// Validate transport
	validTransports := []string{"stdio", "streamable", "inmemory"}
	isValidTransport := false
	for _, transport := range validTransports {
		if config.DefaultTransport == transport {
			isValidTransport = true
			break
		}
	}
	if !isValidTransport {
		errors = append(errors, fmt.Sprintf("Invalid default transport: %s (must be one of %v)",
			config.DefaultTransport, validTransports))
	}

	// Validate timeout
	if config.Timeout <= 0 {
		errors = append(errors, "Timeout must be positive")
	}

	// Validate max retries
	if config.MaxRetries < 0 {
		errors = append(errors, "MaxRetries cannot be negative")
	}

	// Validate servers
	for i, server := range config.MCPServers {
		serverErrors := validateServerConfig(server, i)
		errors = append(errors, serverErrors...)
	}

	return errors
}

// validateServerConfig validates a server configuration
func validateServerConfig(server MCPServerConfig, index int) []string {
	var errors []string
	prefix := fmt.Sprintf("Server %d", index+1)

	if server.Name == "" {
		errors = append(errors, fmt.Sprintf("%s: Name is required", prefix))
	}

	switch server.Transport {
	case "stdio":
		if len(server.Command) == 0 {
			errors = append(errors, fmt.Sprintf("%s: Command is required for stdio transport", prefix))
		}
	case "streamable":
		if server.URL == "" {
			errors = append(errors, fmt.Sprintf("%s: URL is required for streamable transport", prefix))
		}
	case "":
		errors = append(errors, fmt.Sprintf("%s: Transport is required", prefix))
	default:
		errors = append(errors, fmt.Sprintf("%s: Invalid transport '%s'", prefix, server.Transport))
	}

	return errors
}

// GenerateExampleConfig generates an example configuration file
func GenerateExampleConfig(path string) error {
	mcpCfg := &MCPConfig{
		EnableClient:     true,
		DefaultTransport: "stdio",
		Timeout:          30 * time.Second,
		MaxRetries:       3,
		EnableAuth:       false,
		AuthTokens:       make(map[string]string),
		TLSConfig: &TLSConfig{
			Enabled:    false,
			SkipVerify: false,
		},
		MCPServers: []MCPServerConfig{
			{
				Name:        "example-filesystem",
				Description: "Example filesystem server",
				Transport:   "stdio",
				Command:     []string{"npx", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/directory"},
				Enabled:     false,
				Timeout:     30 * time.Second,
				Headers:     make(map[string]string),
			},
			{
				Name:        "example-streamable",
				Description: "Example Streamable HTTP server",
				Transport:   "streamable",
				URL:         "https://example.com/mcp",
				Enabled:     false,
				Timeout:     30 * time.Second,
				Headers:     make(map[string]string),
			},
		},
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Build top-level YAML structure
	out := struct {
		EnableMCP bool              `yaml:"enable_mcp"`
		MCP       *config.MCPConfig `yaml:"mcp"`
	}{
		EnableMCP: true,
		MCP:       convertToConfigMCP(mcpCfg),
	}

	// Marshal to YAML
	data, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}

	logger.Infof("✓ Example configuration saved to: %s", path)
	return nil
}
