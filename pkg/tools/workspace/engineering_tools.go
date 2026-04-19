// Package workspace implements workspace management tools
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"gopkg.in/yaml.v2"
)

// EngineeringToolsTool implements various software engineering tools
type EngineeringToolsTool struct {
	workingDir string
	config     map[string]interface{}
}

// NewEngineeringToolsTool creates a new EngineeringToolsTool instance
func NewEngineeringToolsTool(workingDir string, config map[string]interface{}) *EngineeringToolsTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	return &EngineeringToolsTool{
		workingDir: workingDir,
		config:     config,
	}
}

// Name returns the tool name
func (t *EngineeringToolsTool) Name() string {
	return "engineering_tools"
}

// Description returns the tool description
func (t *EngineeringToolsTool) Description() string {
	return "Software engineering tools for project scaffolding, testing, and analysis"
}

// Category returns the tool category
func (t *EngineeringToolsTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}

// RequiresConfirmation checks if the tool requires confirmation
func (t *EngineeringToolsTool) RequiresConfirmation() bool {
	return true // Most operations modify files
}

// ConcurrencySafe returns false: engineering tools can scaffold files and run tests.
func (t *EngineeringToolsTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *EngineeringToolsTool) Schema() *interfaces.ToolSchema {
	actionProp := interfaces.NewStringPropertyWithEnum(
		"Action to perform",
		[]string{"create_template", "run_tests", "analyze_deps", "format_code"},
	)
	actionProp.Examples = []string{"create_template", "run_tests"}
	actionProp.Usage = "create_template: Create new project from template; run_tests: Run project tests; analyze_deps: Analyze dependencies; format_code: Format source code"

	// create_template params
	projectTypeProp := interfaces.NewStringPropertyWithEnum(
		"Project type (for create_template)",
		[]string{"go", "python", "node", "react", "fastapi", "django", "flask", "express"},
	)
	projectTypeProp.Examples = []string{"go", "python", "node"}
	projectTypeProp.Usage = "Type of project to scaffold"

	projectNameProp := interfaces.NewStringProperty("Project name (for create_template)")
	projectNameProp.Examples = []string{"my-app", "api-service"}
	projectNameProp.Usage = "Name of the new project directory"

	// run_tests params
	testPathProp := interfaces.NewStringProperty("Test path or pattern (optional)")
	testPathProp.Examples = []string{"./...", "tests/", "pkg/utils"}
	testPathProp.Usage = "Specific path to run tests in"

	verboseProp := interfaces.NewBooleanProperty("Verbose output (optional)")
	verboseProp.Usage = "Enable verbose output for tests or analysis"

	return interfaces.CreateSchema(
		"Software engineering utilities",
		map[string]*interfaces.PropertySchema{
			"action":       actionProp,
			"project_type": projectTypeProp,
			"project_name": projectNameProp,
			"test_path":    testPathProp,
			"verbose":      verboseProp,
		},
		[]string{"action"},
	)
}

// Execute executes the engineering tools tool
func (t *EngineeringToolsTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to execute engineering operation: tool parameters are missing",
			LLMContent:  "engineering_tools failed: tool parameters are missing",
		}, nil
	}

	// Extract action
	action, ok := params["action"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "action parameter is required and must be a string",
			UserContent: "❌ Failed to execute engineering operation: action parameter is required",
			LLMContent:  "engineering_tools failed: action parameter is required",
		}, nil
	}

	switch action {
	case "create_template":
		return t.createTemplate(ctx, params)
	case "init_project":
		return t.initProject(ctx, params)
	case "install_deps":
		return t.installDependencies(ctx, params)
	case "update_deps":
		return t.updateDependencies(ctx, params)
	case "build":
		return t.buildProject(ctx, params)
	case "test":
		return t.runTests(ctx, params)
	case "lint":
		return t.lintCode(ctx, params)
	case "format":
		return t.formatCode(ctx, params)
	case "analyze":
		return t.analyzeCode(ctx, params)
	case "diagnose":
		return t.diagnose(ctx, params)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported action: %s", action),
			UserContent: fmt.Sprintf("❌ Unsupported engineering action: %s", action),
			LLMContent:  fmt.Sprintf("engineering_tools failed: unsupported action %s", action),
		}, nil
	}
}

func (t *EngineeringToolsTool) getTargetDir(params map[string]interface{}) string {
	if targetDir, ok := params["target_dir"].(string); ok && targetDir != "" {
		return targetDir
	}
	return t.workingDir
}

func (t *EngineeringToolsTool) createTemplate(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	projectType, ok := params["project_type"].(string)
	if !ok || projectType == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "project_type is required for create_template action",
			UserContent: "❌ Project type is required for template creation",
			LLMContent:  "engineering_tools create_template failed: project_type is required",
		}, nil
	}

	projectName, ok := params["project_name"].(string)
	if !ok || projectName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "project_name is required for create_template action",
			UserContent: "❌ Project name is required for template creation",
			LLMContent:  "engineering_tools create_template failed: project_name is required",
		}, nil
	}

	targetDir := t.getTargetDir(params)
	projectPath := filepath.Join(targetDir, projectName)

	// Check if directory already exists
	if _, err := os.Stat(projectPath); err == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "project directory already exists",
			UserContent: fmt.Sprintf("❌ Project directory already exists: %s", projectPath),
			LLMContent:  "engineering_tools create_template failed: project directory already exists",
		}, nil
	}

	var cmd *exec.Cmd
	var cmdArgs []string

	switch projectType {
	case "react":
		cmd = exec.CommandContext(ctx, "npx", "create-react-app", projectName)
	case "vue":
		cmd = exec.CommandContext(ctx, "npm", "create", "vue@latest", projectName)
	case "angular":
		cmd = exec.CommandContext(ctx, "npx", "@angular/cli", "new", projectName, "--routing", "--style=css")
	case "nextjs":
		cmd = exec.CommandContext(ctx, "npx", "create-next-app@latest", projectName)
	case "nuxt":
		cmd = exec.CommandContext(ctx, "npx", "nuxi@latest", "init", projectName)
	case "svelte":
		cmd = exec.CommandContext(ctx, "npm", "create", "svelte@latest", projectName)
	case "node", "express":
		return t.createNodeTemplate(ctx, projectName, projectPath, projectType)
	case "go":
		return t.createGoTemplate(ctx, projectName, projectPath)
	case "rust":
		cmd = exec.CommandContext(ctx, "cargo", "new", projectName)
	case "python", "fastapi", "django", "flask":
		return t.createPythonTemplate(ctx, projectName, projectPath, projectType)
	case "java", "spring":
		return t.createJavaTemplate(ctx, projectName, projectPath, projectType)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported project type: %s", projectType),
			UserContent: fmt.Sprintf("❌ Unsupported project type: %s", projectType),
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: unsupported project type %s", projectType),
		}, nil
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("template creation failed: %v\nOutput: %s", err, string(output)),
			UserContent: fmt.Sprintf("❌ Failed to create %s template: %v", projectType, err),
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"project_type": projectType,
			"project_name": projectName,
			"project_path": projectPath,
			"command":      strings.Join(cmdArgs, " "),
			"duration":     duration.String(),
			"output":       string(output),
		},
		UserContent: fmt.Sprintf("✅ %s template created successfully\n  📁 Project: %s\n  📍 Location: %s\n  ⏱️ Duration: %s", strings.Title(projectType), projectName, projectPath, duration), //nolint:staticcheck
		LLMContent:  fmt.Sprintf("engineering_tools create_template successful: created %s project %s at %s", projectType, projectName, projectPath),
	}, nil
}

func (t *EngineeringToolsTool) createNodeTemplate(_ context.Context, projectName, projectPath, projectType string) (*interfaces.ToolResult, error) {
	// Create directory
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create directory: %v", err),
			UserContent: "❌ Failed to create project directory",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create package.json
	packageJson := map[string]interface{}{ //nolint:revive
		"name":        projectName,
		"version":     "1.0.0",
		"description": fmt.Sprintf("A %s application", projectType),
		"main":        "index.js",
		"scripts": map[string]string{
			"start": "node index.js",
			"dev":   "nodemon index.js",
			"test":  "jest",
		},
		"keywords": []string{projectType, "nodejs"},
		"author":   "",
		"license":  "MIT",
	}

	if projectType == "express" {
		packageJson["dependencies"] = map[string]string{
			"express": "^4.18.0",
		}
		packageJson["devDependencies"] = map[string]string{
			"nodemon": "^3.0.0",
			"jest":    "^29.0.0",
		}
	}

	packageJsonBytes, _ := json.MarshalIndent(packageJson, "", "  ") //nolint:revive
	if err := os.WriteFile(filepath.Join(projectPath, "package.json"), packageJsonBytes, 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create package.json: %v", err),
			UserContent: "❌ Failed to create package.json",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create index.js
	var indexContent string
	if projectType == "express" {
		indexContent = `const express = require('express');
const app = express();
const port = process.env.PORT || 3000;

app.get('/', (req, res) => {
  res.json({ message: 'Hello World!' });
});

app.listen(port, () => {
  console.log(` + "`Server running on port ${port}`" + `);
});`
	} else {
		indexContent = `console.log('Hello World!');
`
	}

	if err := os.WriteFile(filepath.Join(projectPath, "index.js"), []byte(indexContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create index.js: %v", err),
			UserContent: "❌ Failed to create index.js",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create README.md
	readmeContent := fmt.Sprintf(`# %s

A %s application.

## Installation

`+"```bash\nnpm install\n```"+`

## Usage

`+"```bash\nnpm start\n```"+`

## Development

`+"```bash\nnpm run dev\n```"+`
`, projectName, projectType)

	if err := os.WriteFile(filepath.Join(projectPath, "README.md"), []byte(readmeContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create README.md: %v", err),
			UserContent: "❌ Failed to create README.md",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"project_type":  projectType,
			"project_name":  projectName,
			"project_path":  projectPath,
			"files_created": []string{"package.json", "index.js", "README.md"},
		},
		UserContent: fmt.Sprintf("✅ %s template created successfully\n  📁 Project: %s\n  📍 Location: %s\n  📄 Files: package.json, index.js, README.md", strings.Title(projectType), projectName, projectPath), //nolint:staticcheck
		LLMContent:  fmt.Sprintf("engineering_tools create_template successful: created %s project %s at %s", projectType, projectName, projectPath),
	}, nil
}

func (t *EngineeringToolsTool) createGoTemplate(ctx context.Context, projectName, projectPath string) (*interfaces.ToolResult, error) {
	// Create directory
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create directory: %v", err),
			UserContent: "❌ Failed to create project directory",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Initialize go module
	cmd := exec.CommandContext(ctx, "go", "mod", "init", projectName)
	cmd.Dir = projectPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("go mod init failed: %v\nOutput: %s", err, string(output)),
			UserContent: "❌ Failed to initialize Go module",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create main.go
	mainContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`

	if err := os.WriteFile(filepath.Join(projectPath, "main.go"), []byte(mainContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create main.go: %v", err),
			UserContent: "❌ Failed to create main.go",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create README.md
	readmeContent := fmt.Sprintf(`# %s

A Go application.

## Build

`+"```bash\ngo build\n```"+`

## Run

`+"```bash\ngo run main.go\n```"+`

## Test

`+"```bash\ngo test\n```"+`
`, projectName)

	if err := os.WriteFile(filepath.Join(projectPath, "README.md"), []byte(readmeContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create README.md: %v", err),
			UserContent: "❌ Failed to create README.md",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"project_type":  "go",
			"project_name":  projectName,
			"project_path":  projectPath,
			"files_created": []string{"go.mod", "main.go", "README.md"},
		},
		UserContent: fmt.Sprintf("✅ Go template created successfully\n  📁 Project: %s\n  📍 Location: %s\n  📄 Files: go.mod, main.go, README.md", projectName, projectPath),
		LLMContent:  fmt.Sprintf("engineering_tools create_template successful: created go project %s at %s", projectName, projectPath),
	}, nil
}

func (t *EngineeringToolsTool) createPythonTemplate(_ context.Context, projectName, projectPath, projectType string) (*interfaces.ToolResult, error) {
	// Create directory structure
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create directory: %v", err),
			UserContent: "❌ Failed to create project directory",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create requirements.txt
	var requirements string
	switch projectType {
	case "fastapi":
		requirements = "fastapi>=0.104.0\nuvicorn[standard]>=0.24.0\n"
	case "django":
		requirements = "Django>=4.2.0\n"
	case "flask":
		requirements = "Flask>=2.3.0\n"
	default:
		requirements = "# Add your dependencies here\n"
	}

	if err := os.WriteFile(filepath.Join(projectPath, "requirements.txt"), []byte(requirements), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create requirements.txt: %v", err),
			UserContent: "❌ Failed to create requirements.txt",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create main application file
	var mainContent string
	var mainFile string

	switch projectType {
	case "fastapi":
		mainFile = "main.py"
		mainContent = `from fastapi import FastAPI

app = FastAPI()

@app.get("/")
def read_root():
    return {"Hello": "World"}

@app.get("/items/{item_id}")
def read_item(item_id: int, q: str = None):
    return {"item_id": item_id, "q": q}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
`
	case "django":
		// For Django, we'll create a simple manage.py and settings
		mainFile = "manage.py"
		mainContent = `#!/usr/bin/env python
import os
import sys

if __name__ == "__main__":
    os.environ.setdefault("DJANGO_SETTINGS_MODULE", "settings")
    try:
        from django.core.management import execute_from_command_line
    except ImportError as exc:
        raise ImportError(
            "Couldn't import Django. Are you sure it's installed and "
            "available on your PYTHONPATH environment variable? Did you "
            "forget to activate a virtual environment?"
        ) from exc
    execute_from_command_line(sys.argv)
`
	case "flask":
		mainFile = "app.py"
		mainContent = `from flask import Flask

app = Flask(__name__)

@app.route('/')
def hello_world():
    return {'message': 'Hello, World!'}

@app.route('/api/health')
def health_check():
    return {'status': 'healthy'}

if __name__ == '__main__':
    app.run(debug=True)
`
	default:
		mainFile = "main.py"
		mainContent = `def main():
    print("Hello, World!")

if __name__ == "__main__":
    main()
`
	}

	if err := os.WriteFile(filepath.Join(projectPath, mainFile), []byte(mainContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create %s: %v", mainFile, err),
			UserContent: fmt.Sprintf("❌ Failed to create %s", mainFile),
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create README.md
	readmeContent := fmt.Sprintf(`# %s

A %s application.

## Setup

`+"```bash\npip install -r requirements.txt\n```"+`

## Run

`+"```bash\npython %s\n```"+`
`, projectName, projectType, mainFile)

	if err := os.WriteFile(filepath.Join(projectPath, "README.md"), []byte(readmeContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create README.md: %v", err),
			UserContent: "❌ Failed to create README.md",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	filesCreated := []string{"requirements.txt", mainFile, "README.md"}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"project_type":  projectType,
			"project_name":  projectName,
			"project_path":  projectPath,
			"files_created": filesCreated,
			"main_file":     mainFile,
		},
		UserContent: fmt.Sprintf("✅ %s template created successfully\n  📁 Project: %s\n  📍 Location: %s\n  📄 Files: %s", strings.Title(projectType), projectName, projectPath, strings.Join(filesCreated, ", ")), //nolint:staticcheck
		LLMContent:  fmt.Sprintf("engineering_tools create_template successful: created %s project %s at %s", projectType, projectName, projectPath),
	}, nil
}

func (t *EngineeringToolsTool) createJavaTemplate(_ context.Context, projectName, projectPath, projectType string) (*interfaces.ToolResult, error) {
	// Create directory structure
	if err := os.MkdirAll(filepath.Join(projectPath, "src", "main", "java"), 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create directory structure: %v", err),
			UserContent: "❌ Failed to create project directory structure",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create pom.xml for Maven
	pomContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>

    <groupId>com.example</groupId>
    <artifactId>%s</artifactId>
    <version>1.0.0</version>
    <packaging>jar</packaging>

    <name>%s</name>
    <description>A %s application</description>

    <properties>
        <maven.compiler.source>11</maven.compiler.source>
        <maven.compiler.target>11</maven.compiler.target>
        <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
    </properties>

    <dependencies>
        <dependency>
            <groupId>junit</groupId>
            <artifactId>junit</artifactId>
            <version>4.13.2</version>
            <scope>test</scope>
        </dependency>
    </dependencies>

    <build>
        <plugins>
            <plugin>
                <groupId>org.apache.maven.plugins</groupId>
                <artifactId>maven-compiler-plugin</artifactId>
                <version>3.11.0</version>
                <configuration>
                    <source>11</source>
                    <target>11</target>
                </configuration>
            </plugin>
        </plugins>
    </build>
</project>
`, projectName, projectName, projectType)

	if err := os.WriteFile(filepath.Join(projectPath, "pom.xml"), []byte(pomContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create pom.xml: %v", err),
			UserContent: "❌ Failed to create pom.xml",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create Main.java
	mainContent := `package com.example;

public class Main {
    public static void main(String[] args) {
        System.out.println("Hello, World!");
    }
}
`

	if err := os.WriteFile(filepath.Join(projectPath, "src", "main", "java", "Main.java"), []byte(mainContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create Main.java: %v", err),
			UserContent: "❌ Failed to create Main.java",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	// Create README.md
	readmeContent := fmt.Sprintf(`# %s

A %s application.

## Build

`+"```bash\nmvn compile\n```"+`

## Run

`+"```bash\nmvn exec:java -Dexec.mainClass=\"com.example.Main\"\n```"+`

## Test

`+"```bash\nmvn test\n```"+`
`, projectName, projectType)

	if err := os.WriteFile(filepath.Join(projectPath, "README.md"), []byte(readmeContent), 0644); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create README.md: %v", err),
			UserContent: "❌ Failed to create README.md",
			LLMContent:  fmt.Sprintf("engineering_tools create_template failed: %v", err),
		}, nil
	}

	filesCreated := []string{"pom.xml", "src/main/java/Main.java", "README.md"}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"project_type":  projectType,
			"project_name":  projectName,
			"project_path":  projectPath,
			"files_created": filesCreated,
		},
		UserContent: fmt.Sprintf("✅ %s template created successfully\n  📁 Project: %s\n  📍 Location: %s\n  📄 Files: %s", strings.Title(projectType), projectName, projectPath, strings.Join(filesCreated, ", ")), //nolint:staticcheck
		LLMContent:  fmt.Sprintf("engineering_tools create_template successful: created %s project %s at %s", projectType, projectName, projectPath),
	}, nil
}

func (t *EngineeringToolsTool) initProject(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// This is similar to create_template but for existing directories
	return t.createTemplate(ctx, params)
}

// DiagnosticInput represents the structure for diagnostic commands
type DiagnosticInput struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// diagnose runs diagnostic commands for different project types
func (t *EngineeringToolsTool) diagnose(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	diagnosticCommand, ok := params["diagnostic_command"].(string)
	if !ok || diagnosticCommand == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "diagnostic_command is required for diagnose action",
			UserContent: "❌ Diagnostic command is required",
			LLMContent:  "engineering_tools diagnose failed: diagnostic_command is required",
		}, nil
	}

	var input DiagnosticInput
	err := yaml.Unmarshal([]byte(diagnosticCommand), &input)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to parse diagnostic command: %v", err),
			UserContent: "❌ Failed to parse diagnostic command",
			LLMContent:  fmt.Sprintf("engineering_tools diagnose failed: %v", err),
		}, nil
	}

	if input.Command == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "command is required in diagnostic_command",
			UserContent: "❌ Command is required in diagnostic_command",
			LLMContent:  "engineering_tools diagnose failed: command is required",
		}, nil
	}

	// Validate supported commands
	supportedCommands := map[string]bool{
		"go":         true,
		"eslint":     true,
		"jest":       true,
		"vitest":     true,
		"flake8":     true,
		"pytest":     true,
		"checkstyle": true,
		"mvn":        true,
	}

	if !supportedCommands[input.Command] {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported diagnostic command: %s", input.Command),
			UserContent: fmt.Sprintf("❌ Unsupported diagnostic command: %s", input.Command),
			LLMContent:  fmt.Sprintf("engineering_tools diagnose failed: unsupported command %s", input.Command),
		}, nil
	}

	targetDir := t.getTargetDir(params)

	// Execute the diagnostic command
	cmd := exec.CommandContext(ctx, input.Command, input.Args...)
	cmd.Dir = targetDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	output := stdout.String()
	if output != "" {
		output = "stdout:\n" + output
	}

	if stderr.String() != "" {
		if output != "" {
			output += "\n"
		}
		output += "stderr:\n" + stderr.String()
	}

	success := err == nil
	statusIcon := "✅"
	if !success {
		statusIcon = "⚠️"
	}

	return &interfaces.ToolResult{
		Success: true, // Always return success for diagnostic commands
		Data: map[string]interface{}{
			"command":    input.Command,
			"args":       input.Args,
			"target_dir": targetDir,
			"duration":   duration.String(),
			"output":     output,
			"passed":     success,
		},
		UserContent: fmt.Sprintf("%s Diagnostic command completed\n  🔧 Command: %s %v\n  ⏱️ Duration: %s", statusIcon, input.Command, input.Args, duration),
		LLMContent:  fmt.Sprintf("engineering_tools diagnose completed: %s %v", input.Command, input.Args),
	}, nil
}

func (t *EngineeringToolsTool) detectPackageManager(targetDir string) string {
	// Check for package manager files
	if _, err := os.Stat(filepath.Join(targetDir, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(targetDir, "yarn.lock")); err == nil {
			return "yarn"
		}
		if _, err := os.Stat(filepath.Join(targetDir, "pnpm-lock.yaml")); err == nil {
			return "pnpm"
		}
		return "npm"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "requirements.txt")); err == nil {
		return "pip"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "pyproject.toml")); err == nil {
		return "poetry"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "go.mod")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "Cargo.toml")); err == nil {
		return "cargo"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "pom.xml")); err == nil {
		return "maven"
	}
	if _, err := os.Stat(filepath.Join(targetDir, "build.gradle")); err == nil {
		return "gradle"
	}
	return "unknown"
}

func (t *EngineeringToolsTool) installDependencies(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Detect or get package manager
	packageManager := t.detectPackageManager(targetDir)
	if pm, ok := params["package_manager"].(string); ok && pm != "" {
		packageManager = pm
	}

	if packageManager == "unknown" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "could not detect package manager",
			UserContent: "❌ Could not detect package manager. Please specify package_manager parameter.",
			LLMContent:  "engineering_tools install_deps failed: could not detect package manager",
		}, nil
	}

	// Get packages to install
	var packages []string
	if packagesParam, ok := params["packages"].([]interface{}); ok {
		for _, pkg := range packagesParam {
			if pkgStr, ok := pkg.(string); ok {
				packages = append(packages, pkgStr)
			}
		}
	}

	var cmd *exec.Cmd
	switch packageManager {
	case "npm":
		if len(packages) > 0 {
			args := append([]string{"install"}, packages...)
			cmd = exec.CommandContext(ctx, "npm", args...)
		} else {
			cmd = exec.CommandContext(ctx, "npm", "install")
		}
	case "yarn":
		if len(packages) > 0 {
			args := append([]string{"add"}, packages...)
			cmd = exec.CommandContext(ctx, "yarn", args...)
		} else {
			cmd = exec.CommandContext(ctx, "yarn", "install")
		}
	case "pnpm":
		if len(packages) > 0 {
			args := append([]string{"add"}, packages...)
			cmd = exec.CommandContext(ctx, "pnpm", args...)
		} else {
			cmd = exec.CommandContext(ctx, "pnpm", "install")
		}
	case "pip":
		if len(packages) > 0 {
			args := append([]string{"install"}, packages...)
			cmd = exec.CommandContext(ctx, "pip", args...)
		} else {
			cmd = exec.CommandContext(ctx, "pip", "install", "-r", "requirements.txt")
		}
	case "poetry":
		if len(packages) > 0 {
			args := append([]string{"add"}, packages...)
			cmd = exec.CommandContext(ctx, "poetry", args...)
		} else {
			cmd = exec.CommandContext(ctx, "poetry", "install")
		}
	case "go":
		if len(packages) > 0 {
			args := append([]string{"get"}, packages...)
			cmd = exec.CommandContext(ctx, "go", args...)
		} else {
			cmd = exec.CommandContext(ctx, "go", "mod", "download")
		}
	case "cargo":
		if len(packages) > 0 {
			args := append([]string{"add"}, packages...)
			cmd = exec.CommandContext(ctx, "cargo", args...)
		} else {
			cmd = exec.CommandContext(ctx, "cargo", "build")
		}
	case "maven":
		cmd = exec.CommandContext(ctx, "mvn", "dependency:resolve")
	case "gradle":
		cmd = exec.CommandContext(ctx, "./gradlew", "dependencies")
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported package manager: %s", packageManager),
			UserContent: fmt.Sprintf("❌ Unsupported package manager: %s", packageManager),
			LLMContent:  fmt.Sprintf("engineering_tools install_deps failed: unsupported package manager %s", packageManager),
		}, nil
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("dependency installation failed: %v\nOutput: %s", err, string(output)),
			UserContent: fmt.Sprintf("❌ Failed to install dependencies: %v", err),
			LLMContent:  fmt.Sprintf("engineering_tools install_deps failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	userContent := "✅ Dependencies installed successfully\n"
	userContent += fmt.Sprintf("  📦 Package Manager: %s\n", packageManager)
	if len(packages) > 0 {
		userContent += fmt.Sprintf("  📋 Packages: %s\n", strings.Join(packages, ", "))
	} else {
		userContent += "  📋 All dependencies from manifest\n"
	}
	userContent += fmt.Sprintf("  ⏱️ Duration: %s", duration)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"package_manager": packageManager,
			"packages":        packages,
			"target_dir":      targetDir,
			"duration":        duration.String(),
			"output":          string(output),
		},
		UserContent: userContent,
		LLMContent:  fmt.Sprintf("engineering_tools install_deps successful: installed dependencies using %s", packageManager),
	}, nil
}

func (t *EngineeringToolsTool) updateDependencies(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Detect or get package manager
	packageManager := t.detectPackageManager(targetDir)
	if pm, ok := params["package_manager"].(string); ok && pm != "" {
		packageManager = pm
	}

	if packageManager == "unknown" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "could not detect package manager",
			UserContent: "❌ Could not detect package manager. Please specify package_manager parameter.",
			LLMContent:  "engineering_tools update_deps failed: could not detect package manager",
		}, nil
	}

	var cmd *exec.Cmd
	switch packageManager {
	case "npm":
		cmd = exec.CommandContext(ctx, "npm", "update")
	case "yarn":
		cmd = exec.CommandContext(ctx, "yarn", "upgrade")
	case "pnpm":
		cmd = exec.CommandContext(ctx, "pnpm", "update")
	case "pip":
		cmd = exec.CommandContext(ctx, "pip", "install", "--upgrade", "-r", "requirements.txt")
	case "poetry":
		cmd = exec.CommandContext(ctx, "poetry", "update")
	case "go":
		cmd = exec.CommandContext(ctx, "go", "get", "-u", "all")
	case "cargo":
		cmd = exec.CommandContext(ctx, "cargo", "update")
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported package manager: %s", packageManager),
			UserContent: fmt.Sprintf("❌ Unsupported package manager: %s", packageManager),
			LLMContent:  fmt.Sprintf("engineering_tools update_deps failed: unsupported package manager %s", packageManager),
		}, nil
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("dependency update failed: %v\nOutput: %s", err, string(output)),
			UserContent: fmt.Sprintf("❌ Failed to update dependencies: %v", err),
			LLMContent:  fmt.Sprintf("engineering_tools update_deps failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"package_manager": packageManager,
			"target_dir":      targetDir,
			"duration":        duration.String(),
			"output":          string(output),
		},
		UserContent: fmt.Sprintf("✅ Dependencies updated successfully\n  📦 Package Manager: %s\n  ⏱️ Duration: %s", packageManager, duration),
		LLMContent:  fmt.Sprintf("engineering_tools update_deps successful: updated dependencies using %s", packageManager),
	}, nil
}

func (t *EngineeringToolsTool) buildProject(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Get custom build command or detect
	var cmd *exec.Cmd
	if buildCmd, ok := params["build_command"].(string); ok && buildCmd != "" {
		parts := strings.Fields(buildCmd)
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		// Auto-detect build command
		packageManager := t.detectPackageManager(targetDir)
		switch packageManager {
		case "npm", "yarn", "pnpm":
			cmd = exec.CommandContext(ctx, packageManager, "run", "build")
		case "go":
			cmd = exec.CommandContext(ctx, "go", "build")
		case "cargo":
			cmd = exec.CommandContext(ctx, "cargo", "build")
		case "maven":
			cmd = exec.CommandContext(ctx, "mvn", "compile")
		case "gradle":
			cmd = exec.CommandContext(ctx, "./gradlew", "build")
		default:
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "could not detect build system",
				UserContent: "❌ Could not detect build system. Please specify build_command parameter.",
				LLMContent:  "engineering_tools build failed: could not detect build system",
			}, nil
		}
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("build failed: %v\nOutput: %s", err, string(output)),
			UserContent: fmt.Sprintf("❌ Build failed: %v", err),
			LLMContent:  fmt.Sprintf("engineering_tools build failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"command":    strings.Join(cmd.Args, " "),
			"target_dir": targetDir,
			"duration":   duration.String(),
			"output":     string(output),
		},
		UserContent: fmt.Sprintf("✅ Build completed successfully\n  🔨 Command: %s\n  ⏱️ Duration: %s", strings.Join(cmd.Args, " "), duration),
		LLMContent:  fmt.Sprintf("engineering_tools build successful: built project in %s", duration),
	}, nil
}

func (t *EngineeringToolsTool) runTests(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Get custom test command or detect
	var cmd *exec.Cmd
	if testCmd, ok := params["test_command"].(string); ok && testCmd != "" {
		parts := strings.Fields(testCmd)
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		// Auto-detect test command
		packageManager := t.detectPackageManager(targetDir)
		switch packageManager {
		case "npm", "yarn", "pnpm":
			cmd = exec.CommandContext(ctx, packageManager, "test")
		case "go":
			cmd = exec.CommandContext(ctx, "go", "test", "./...")
		case "cargo":
			cmd = exec.CommandContext(ctx, "cargo", "test")
		case "pip", "poetry":
			cmd = exec.CommandContext(ctx, "pytest")
		case "maven":
			cmd = exec.CommandContext(ctx, "mvn", "test")
		case "gradle":
			cmd = exec.CommandContext(ctx, "./gradlew", "test")
		default:
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "could not detect test system",
				UserContent: "❌ Could not detect test system. Please specify test_command parameter.",
				LLMContent:  "engineering_tools test failed: could not detect test system",
			}, nil
		}
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	// Tests might fail but still provide useful output
	success := err == nil
	statusIcon := "✅"
	if !success {
		statusIcon = "❌"
	}

	return &interfaces.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"command":    strings.Join(cmd.Args, " "),
			"target_dir": targetDir,
			"duration":   duration.String(),
			"output":     string(output),
			"passed":     success,
		},
		UserContent: fmt.Sprintf("%s Tests completed\n  🧪 Command: %s\n  ⏱️ Duration: %s", statusIcon, strings.Join(cmd.Args, " "), duration),
		LLMContent:  fmt.Sprintf("engineering_tools test completed: %s", strings.Join(cmd.Args, " ")),
	}, nil
}

func (t *EngineeringToolsTool) lintCode(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Get lint tool or detect
	var cmd *exec.Cmd
	if lintTool, ok := params["lint_tool"].(string); ok && lintTool != "" {
		switch lintTool {
		case "eslint":
			cmd = exec.CommandContext(ctx, "npx", "eslint", ".")
		case "prettier":
			cmd = exec.CommandContext(ctx, "npx", "prettier", "--check", ".")
		case "golangci-lint":
			cmd = exec.CommandContext(ctx, "golangci-lint", "run")
		case "pylint":
			cmd = exec.CommandContext(ctx, "pylint", "**/*.py")
		case "flake8":
			cmd = exec.CommandContext(ctx, "flake8", ".")
		case "black":
			cmd = exec.CommandContext(ctx, "black", "--check", ".")
		case "rustfmt":
			cmd = exec.CommandContext(ctx, "cargo", "fmt", "--check")
		case "clippy":
			cmd = exec.CommandContext(ctx, "cargo", "clippy")
		default:
			return &interfaces.ToolResult{
				Success:     false,
				Error:       fmt.Sprintf("unsupported lint tool: %s", lintTool),
				UserContent: fmt.Sprintf("❌ Unsupported lint tool: %s", lintTool),
				LLMContent:  fmt.Sprintf("engineering_tools lint failed: unsupported lint tool %s", lintTool),
			}, nil
		}
	} else {
		// Auto-detect lint tool
		packageManager := t.detectPackageManager(targetDir)
		switch packageManager {
		case "npm", "yarn", "pnpm":
			cmd = exec.CommandContext(ctx, "npx", "eslint", ".")
		case "go":
			cmd = exec.CommandContext(ctx, "golangci-lint", "run")
		case "cargo":
			cmd = exec.CommandContext(ctx, "cargo", "clippy")
		case "pip", "poetry":
			cmd = exec.CommandContext(ctx, "flake8", ".")
		default:
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "could not detect lint tool",
				UserContent: "❌ Could not detect lint tool. Please specify lint_tool parameter.",
				LLMContent:  "engineering_tools lint failed: could not detect lint tool",
			}, nil
		}
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	// Linting might fail but still provide useful output
	success := err == nil
	statusIcon := "✅"
	if !success {
		statusIcon = "⚠️"
	}

	return &interfaces.ToolResult{
		Success: success,
		Data: map[string]interface{}{
			"command":    strings.Join(cmd.Args, " "),
			"target_dir": targetDir,
			"duration":   duration.String(),
			"output":     string(output),
			"passed":     success,
		},
		UserContent: fmt.Sprintf("%s Code linting completed\n  🔍 Command: %s\n  ⏱️ Duration: %s", statusIcon, strings.Join(cmd.Args, " "), duration),
		LLMContent:  fmt.Sprintf("engineering_tools lint completed: %s", strings.Join(cmd.Args, " ")),
	}, nil
}

func (t *EngineeringToolsTool) formatCode(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Auto-detect format tool
	var cmd *exec.Cmd
	packageManager := t.detectPackageManager(targetDir)
	switch packageManager {
	case "npm", "yarn", "pnpm":
		cmd = exec.CommandContext(ctx, "npx", "prettier", "--write", ".")
	case "go":
		cmd = exec.CommandContext(ctx, "go", "fmt", "./...")
	case "cargo":
		cmd = exec.CommandContext(ctx, "cargo", "fmt")
	case "pip", "poetry":
		cmd = exec.CommandContext(ctx, "black", ".")
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "could not detect format tool",
			UserContent: "❌ Could not detect format tool for this project type.",
			LLMContent:  "engineering_tools format failed: could not detect format tool",
		}, nil
	}

	cmd.Dir = targetDir
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("code formatting failed: %v\nOutput: %s", err, string(output)),
			UserContent: fmt.Sprintf("❌ Code formatting failed: %v", err),
			LLMContent:  fmt.Sprintf("engineering_tools format failed: %v", err),
		}, nil
	}

	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"command":    strings.Join(cmd.Args, " "),
			"target_dir": targetDir,
			"duration":   duration.String(),
			"output":     string(output),
		},
		UserContent: fmt.Sprintf("✅ Code formatted successfully\n  🎨 Command: %s\n  ⏱️ Duration: %s", strings.Join(cmd.Args, " "), duration),
		LLMContent:  fmt.Sprintf("engineering_tools format successful: formatted code using %s", strings.Join(cmd.Args, " ")),
	}, nil
}

func (t *EngineeringToolsTool) analyzeCode(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	targetDir := t.getTargetDir(params)

	// Run multiple analysis tools
	var results []map[string]interface{}
	var allOutput strings.Builder

	// Detect package manager for appropriate tools
	packageManager := t.detectPackageManager(targetDir)

	analysisCommands := []struct {
		name string
		cmd  *exec.Cmd
	}{}

	switch packageManager {
	case "npm", "yarn", "pnpm":
		analysisCommands = append(analysisCommands,
			struct {
				name string
				cmd  *exec.Cmd
			}{"eslint", exec.CommandContext(ctx, "npx", "eslint", ".", "--format", "json")},
			struct {
				name string
				cmd  *exec.Cmd
			}{"audit", exec.CommandContext(ctx, packageManager, "audit")},
		)
	case "go":
		analysisCommands = append(analysisCommands,
			struct {
				name string
				cmd  *exec.Cmd
			}{"vet", exec.CommandContext(ctx, "go", "vet", "./...")},
			struct {
				name string
				cmd  *exec.Cmd
			}{"mod_tidy", exec.CommandContext(ctx, "go", "mod", "tidy")},
		)
	case "cargo":
		analysisCommands = append(analysisCommands,
			struct {
				name string
				cmd  *exec.Cmd
			}{"clippy", exec.CommandContext(ctx, "cargo", "clippy", "--", "-D", "warnings")},
			struct {
				name string
				cmd  *exec.Cmd
			}{"audit", exec.CommandContext(ctx, "cargo", "audit")},
		)
	case "pip", "poetry":
		analysisCommands = append(analysisCommands,
			struct {
				name string
				cmd  *exec.Cmd
			}{"flake8", exec.CommandContext(ctx, "flake8", ".")},
			struct {
				name string
				cmd  *exec.Cmd
			}{"safety", exec.CommandContext(ctx, "safety", "check")},
		)
	}

	start := time.Now()
	for _, analysis := range analysisCommands {
		analysis.cmd.Dir = targetDir
		output, err := analysis.cmd.CombinedOutput()

		result := map[string]interface{}{
			"tool":    analysis.name,
			"command": strings.Join(analysis.cmd.Args, " "),
			"output":  string(output),
			"success": err == nil,
		}

		if err != nil {
			result["error"] = err.Error()
		}

		results = append(results, result)
		fmt.Fprintf(&allOutput, "=== %s ===\n%s\n\n", analysis.name, string(output))
	}
	duration := time.Since(start)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"target_dir": targetDir,
			"duration":   duration.String(),
			"results":    results,
			"output":     allOutput.String(),
		},
		UserContent: fmt.Sprintf("✅ Code analysis completed\n  🔬 Tools run: %d\n  ⏱️ Duration: %s", len(results), duration),
		LLMContent:  fmt.Sprintf("engineering_tools analyze successful: ran %d analysis tools", len(results)),
	}, nil
}
