# Nano Agent Daemon Deployment Guide

[中文](./DEPLOYMENT.zh-CN.md)

This document describes how to deploy the nano agent daemon mode on an AWS EC2 instance, and how to connect to the remote daemon via configuration files.

## Quick Start

### Server Deployment
1. Configure the EC2 address and key path in the deployment scripts
2. Run `./deploy-daemon.sh` to deploy the daemon to EC2
3. Verify the daemon is running
## New Feature: Configurable Image Generator Models

The latest version supports configuring the OpenRouter and Seedream image generator models via environment variables:
- `OPENROUTER_IMAGE_MODEL`: OpenRouter image model name (default: google/gemini-2.5-flash-image)
- `SEEDREAM_IMAGE_MODEL`: Seedream image model name (default: doubao-seedream-4-0-250828)

This allows users to flexibly switch between different image generation models without modifying code.


### Client Connection
1. Copy the client configuration template: `cp deployment/client-config.yaml .nano.yaml`
2. Edit the configuration file to set the daemon server address and port
3. Use `nano "your prompt"` to automatically connect to the daemon

For detailed steps, refer to the sections below.

## Deployment Script Overview

### `unified-deploy.sh` - Unified Deployment Script

This is the unified deployment script, integrating all deployment, monitoring, testing, and failure recovery functionality.

**Main features:**
- Full deployment (build + transfer + configure + start)
- Service management (start/stop/restart)
- Health checks and status monitoring
- Failure diagnosis and automatic repair
- Log viewing and analysis
- SystemD configuration repair

**Supported commands:**
```bash
./unified-deploy.sh deploy    # Full deployment
./unified-deploy.sh restart   # Restart the service
./unified-deploy.sh start     # Start the service
./unified-deploy.sh stop      # Stop the service
./unified-deploy.sh status    # Check status
./unified-deploy.sh test      # Run health checks
./unified-deploy.sh logs      # View logs
./unified-deploy.sh fix       # Repair SystemD configuration
./unified-deploy.sh monitor   # Start monitoring mode
```

## Prerequisites

### 1. Modify Script Configuration

Before using the scripts, modify the following variables:

```bash
# Modify these variables in deploy-daemon.sh and update-daemon.sh
EC2_HOST="your-ec2-instance.compute.amazonaws.com"  # Your EC2 instance address
PEM_FILE="~/Downloads/your-key.pem"                # Path to your PEM key file
```

### 2. Configure API Keys

In `deploy-daemon.sh`, find the configuration file section and set your LLM API key:

```yaml
api_key: "your-llm-api-key"  # Replace with your actual API key
```

### 3. Ensure EC2 Instance Configuration

- The EC2 instance runs Ubuntu
- The security group opens port 8080 (or another port you configured)
- There is sufficient disk space and memory

## Deployment Steps

1. Add execute permission to the scripts:
```bash
chmod +x unified-deploy.sh
```

2. Run the full deployment:
```bash
./unified-deploy.sh deploy
```

3. Check deployment status:
```bash
./unified-deploy.sh status
```

4. Run health checks:
```bash
./unified-deploy.sh test
```

## Daemon Management Commands

### Recommended: Using the Unified Script

```bash
# View daemon status (includes detailed health checks)
./unified-deploy.sh status

# View daemon logs
./unified-deploy.sh logs

# Stop the daemon
./unified-deploy.sh stop

# Start the daemon
./unified-deploy.sh start

# Restart the daemon
./unified-deploy.sh restart

# Run full tests
./unified-deploy.sh test

# Repair SystemD configuration issues
./unified-deploy.sh fix

# Start monitoring mode
./unified-deploy.sh monitor
```

### Alternative: Using nano Commands Directly

After deployment, you can use the following commands on the EC2 instance to manage the daemon:

```bash
# View daemon status
nano daemon status

# View daemon logs
nano daemon logs

# Stop the daemon
nano daemon stop

# Start the daemon
nano daemon start

# Restart the daemon
nano daemon restart
```

## Client Usage

### Configuration File Priority

nano loads configuration files in the following priority order:
1. Configuration file specified on the command line (the `--config` flag)
2. `.nano.yaml` in the project directory
3. Global configuration file `~/.config/nano/config.yaml`
4. Environment variables (highest priority, overriding file configuration)

### Method 1: Connect to the Remote Daemon via Configuration File (Recommended)

1. **Create the client configuration file**:
```bash
# Copy the client configuration template to the global configuration directory
cp deployment/client-config.yaml ~/.config/nano/config.yaml

# Or copy it to the project directory (higher priority)
cp deployment/client-config.yaml .nano.yaml

# Or use a custom configuration file path
cp deployment/client-config.yaml /path/to/my-config.yaml
```

2. **Edit the configuration file** to modify the daemon connection information:
```yaml
# Set the daemon connection information in the configuration file
daemon:
  port: 8080                           # Port of the remote daemon
  host: "your-ec2-instance.com"        # Replace with your EC2 instance address
  api_key: "nano-agent-9527!"          # If the daemon has an authentication key set
```

3. **Use daemon mode**:
```bash
# Automatically detect and use the daemon (if the daemon is running)
nano "your prompt here"

# Force daemon mode
nano --daemon "your prompt here"
nano -d "your prompt here"

# Use a custom configuration file
nano --config /path/to/my-config.yaml "your prompt here"
nano -c /path/to/my-config.yaml "your prompt here"

# Set a timeout (default is 300 seconds)
nano --daemon --timeout 600 "your prompt here"

# Force TUI mode (even if the daemon is running)
nano --tui "your prompt here"
nano -t "your prompt here"
```

### Method 2: Using the client Subcommand

#### Execute Commands
```bash
# Execute a command through the daemon
nano client exec "your prompt here"

# Set a timeout
nano client exec --timeout 600 "your prompt here"
```

#### View Daemon Status
```bash
# View daemon health status and basic information
nano client status
```

#### MCP Management
```bash
# View MCP status
nano client mcp status

# List available MCP tools
nano client mcp tools

# Get MCP diagnostic information
nano client mcp diagnostics
```

#### Memory Management
```bash
# List all memory entries
nano client memory list

# Save a memory entry
nano client memory save "key" "content"

# Get a memory entry
nano client memory get "key"

# Delete a memory entry
nano client memory delete "key"
```

### Method 3: Configuration via Environment Variables

```bash
# Set daemon connection environment variables
export NANO_DAEMON_HOST="your-ec2-instance.com"
export NANO_DAEMON_PORT="8080"
export NANO_DAEMON_API_KEY="nano-agent-9527!"

# Set other configuration
export NANO_API_KEY="your-llm-api-key"
export NANO_BASE_URL="https://api.openai.com/v1"
export NANO_MODEL="gpt-4"
export NANO_VERBOSE="true"
# Image generator configuration (new)
export OPENROUTER_IMAGE_MODEL="google/gemini-2.5-flash-image"  # OpenRouter image model
export SEEDREAM_IMAGE_MODEL="doubao-seedream-4-0-250828"      # Seedream image model
export IMAGE_API_KEY: [REDACTED]                # OpenRouter API key
export SEEDREAM_API_KEY: [REDACTED]               # Seedream API key


# Use daemon mode
nano --daemon "your prompt here"
```

### Viewing Configuration Files

```bash
# View configuration file load order and status
nano config locations

# View using a custom configuration file
nano --config /path/to/config.yaml config locations
```

### Configuration File Locations

**Server side (EC2 instance)**:
- Configuration file: `~/.config/nano/config.yaml`
- PID file: `~/.nano/daemon.pid`
- Log file: `~/.nano/daemon.log`

**Client side (local machine)**:
- Global configuration: `~/.config/nano/config.yaml`
- Project configuration: `.nano.yaml` (project root directory)
- Configuration priority: project configuration > global configuration > environment variables

### Client Configuration Notes

**Important: daemon client configuration limitations**

The daemon client is just an HTTP client; all AI processing happens on the daemon server side. Therefore, most configuration items in the client configuration file **will not take effect**:

❌ **Ineffective configuration items** (ignored in daemon client mode):
- LLM configuration (`api_key`, `base_url`, `model`, etc.)
- Memory system configuration (the `memory` section)
- Tool configuration (`enabled_tools`, `disabled_tools`, etc.)
- MCP configuration (the `mcp` section)
- Web search API keys (`web_search_api_keys`)
- Context management configuration (the `context` section)

✅ **Effective configuration items**:
```yaml
# Client timeout settings
response_timeout: 300s  # Timeout for waiting on daemon responses
http_timeout: 60s       # HTTP request timeout

# Security settings (client local behavior)
confirm_destructive: false

# Daemon connection configuration (most important)
daemon:
  host: "your-ec2-instance.com"    # Remote daemon server address
  port: 8080                       # Daemon listening port
  api_key: "your-api-key"          # Optional: authentication key
  tls_cert_file: ""                # Optional: HTTPS certificate
  tls_key_file: ""                 # Optional: HTTPS private key
```

**Important notes**:
- The client **only needs the `daemon` section configured**; other configuration is handled on the server side
- If the daemon server has an `api_key` set for authentication, the client must configure the same key
- HTTPS connections are supported; the corresponding certificate files need to be configured

### Client Configuration Best Practices

1. **Project-level configuration** (recommended):
```bash
# Create the configuration file in the project root directory
cp deployment/client-config.yaml .nano.yaml
# Edit the configuration, keeping only the daemon section
```

2. **Global configuration**:
```bash
# Create the global configuration directory
mkdir -p ~/.config/nano
# Copy and edit the configuration file
cp deployment/client-config.yaml ~/.config/nano/config.yaml
```

3. **Configuration verification**:
```bash
# Check the daemon connection
nano client status

# View configuration paths
nano config paths
```

4. **Common configuration examples**:

**Local development environment**:
```yaml
daemon:
  host: "127.0.0.1"
  port: 8080
  api_key: ""
```

**Connecting to a remote server**:
```yaml
daemon:
  host: "your-server.example.com"
  port: 8080
  api_key: "your-secure-api-key"
```

**HTTPS connection**:
```yaml
daemon:
  host: "your-server.example.com"
  port: 8443
  api_key: "your-secure-api-key"
  tls_cert_file: "/path/to/client.crt"
  tls_key_file: "/path/to/client.key"
```

## Security Considerations

1. **API key security**: make sure not to commit API keys to version control
2. **Network security**: consider using a VPN or restricting the allowed IP range
3. **Authentication**: you can set an API key in the daemon configuration for authentication
4. **HTTPS**: configuring TLS certificates is recommended for production environments

## Troubleshooting

### Troubleshooting with the Unified Script

1. **Quick diagnosis**
   ```bash
   # Run full tests, including all health checks
   ./unified-deploy.sh test

   # View detailed status information
   ./unified-deploy.sh status
   ```

2. **Automatically repair common issues**
   ```bash
   # Repair SystemD configuration issues
   ./unified-deploy.sh fix

   # Restart the service to resolve temporary issues
   ./unified-deploy.sh restart
   ```

3. **View detailed logs**
   ```bash
   # View daemon logs
   ./unified-deploy.sh logs

   # Start monitoring mode to watch status in real time
   ./unified-deploy.sh monitor
   ```

### Common Issues

**Server deployment issues**:
1. **Connection refused**: check whether the EC2 security group opens the corresponding port
2. **Go not found**: the script will install Go automatically; if that fails, install it manually
3. **Permission issues**: ensure the PEM file has the correct permissions (600)
4. **Port conflict**: if port 8080 is in use, change the port number in the configuration
5. **SystemD service issues**: use `./unified-deploy.sh fix` to automatically repair the configuration

**Client connection issues**:
1. **Unable to connect to the daemon**:
   ```bash
   # Check daemon status
   nano client status
   # Check configuration
   nano config paths
   ```

2. **Authentication failure**:
   - Ensure the `api_key` on the client and server match
   - Check the `daemon.api_key` setting in the configuration file

3. **Configuration file not taking effect**:
   ```bash
   # Check the configuration load order
   nano config paths
   # Ensure the configuration file is in the correct location
   ```

4. **Network connection issues**:
   - Check firewall settings
   - Confirm the server address and port are correct
   - Test network connectivity: `telnet your-server 8080`
   - Use `./unified-deploy.sh status` to verify the daemon is running

5. **HTTPS certificate issues**:
   - Ensure the certificate file paths are correct
   - Check certificate validity
   - Verify the certificate matches the server domain

### Viewing Logs

```bash
# View daemon logs on the EC2 instance
nano daemon logs

# Or view the log file directly
tail -f ~/.nano/daemon.log
```

## Advanced Configuration

### Custom Port and Host

Modify in `deploy-daemon.sh`:
```bash
DAEMON_PORT="8080"     # Change to the port you want
DAEMON_HOST="0.0.0.0"  # 0.0.0.0 listens on all interfaces; 127.0.0.1 is local only
```

### Enabling HTTPS

Add the TLS certificate configuration to the configuration file:
```yaml
daemon:
  tls_cert_file: "/path/to/cert.pem"
  tls_key_file: "/path/to/key.pem"
```

### System Service Configuration

To configure the daemon as a system service, you can create a systemd service file:

```bash
# Create the service file
sudo nano /etc/systemd/system/nano-daemon.service
```

Service file contents:
```ini
[Unit]
Description=Nano Agent Daemon
After=network.target

[Service]
Type=forking
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/usr/local/bin/nano daemon start
ExecStop=/usr/local/bin/nano daemon stop
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable the service:
```bash
sudo systemctl enable nano-daemon
sudo systemctl start nano-daemon
```
