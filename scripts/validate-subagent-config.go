package main //nolint:revive

import (
	"fmt"
	"log"
	"os"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run validate-subagent-config.go <config-file>")
		os.Exit(1)
	}

	configFile := os.Args[1]

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Printf("✅ Configuration loaded successfully from: %s\n", configFile)

	// Summary
	fmt.Printf("\n📊 Configuration Summary:\n")
	fmt.Printf("   - Model: %s\n", cfg.Model)
	fmt.Printf("   - Base URL: %s\n", cfg.BaseURL)

	// Validate required fields
	valid := true
	if cfg.APIKey == "" {
		fmt.Println("❌ api_key is not set")
		valid = false
	} else {
		fmt.Println("✅ api_key is set")
	}
	if cfg.Model == "" {
		fmt.Println("❌ model is not set")
		valid = false
	}

	if valid {
		fmt.Printf("\n✅ Configuration validation completed successfully!\n")
	} else {
		fmt.Printf("\n❌ Configuration validation failed!\n")
		os.Exit(1)
	}
}
