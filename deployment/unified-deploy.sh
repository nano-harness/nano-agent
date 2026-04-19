#!/bin/bash
set -euo pipefail

if [ -f ./.env ]; then
    set -a
    . ./.env
    set +a
fi

# Nano Agent Unified Deployment Script
# Combines deployment, monitoring, testing, and recovery functionality
# Fixes systemd process management issues by using foreground mode

# Variables
EC2_USER="${EC2_USER:-ubuntu}"
EC2_HOST="${EC2_HOST:-ec2-43-200-3-149.ap-northeast-2.compute.amazonaws.com}"
PEM_FILE="${PEM_FILE:-~/Downloads/web-crawler.pem}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/nano-agent}"
LOCAL_PROJECT_DIR="${LOCAL_PROJECT_DIR:-${GITHUB_WORKSPACE:-$(pwd)}}"
BINARY_NAME="${BINARY_NAME:-nano}"
DAEMON_PORT="${DAEMON_PORT:-8080}"
DAEMON_HOST="${DAEMON_HOST:-0.0.0.0}"
TARGET_OS="${TARGET_OS:-linux}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"
API_KEY="${API_KEY:-nano-agent-9527!}"
DAEMON_HOST_EXPORT="${DAEMON_HOST_EXPORT:-0.0.0.0}"

NANO_API_KEY="${NANO_API_KEY:-}"
NANO_BASE_URL="${NANO_BASE_URL:-https://api.deepseek.com/v1}"
NANO_MODEL="${NANO_MODEL:-deepseek-chat}"
NANO_VERBOSE="${NANO_VERBOSE:-true}"
SERPER_API_KEY="${SERPER_API_KEY:-}"
TAVILY_API_KEY="${TAVILY_API_KEY:-}"
OSS_ENABLED="${OSS_ENABLED:-false}"
OSS_ACCESS_KEY_ID="${OSS_ACCESS_KEY_ID:-}"
OSS_ACCESS_KEY_SECRET="${OSS_ACCESS_KEY_SECRET:-}"
OSS_ENDPOINT="${OSS_ENDPOINT:-}"
OSS_DEFAULT_BUCKET="${OSS_DEFAULT_BUCKET:-}"
OSS_REGION="${OSS_REGION:-}"
OSS_TIMEOUT="${OSS_TIMEOUT:-60}"
OSS_CALLBACK_URL="${OSS_CALLBACK_URL:-https://www.ilovehomelibrary.cn/api/v1/internal/ai-assistant/callback}"
OSS_CALLBACK_TOKEN="${OSS_CALLBACK_TOKEN:-nano-agent-callback-2026}"
IMAGE_API_KEY="${IMAGE_API_KEY:-}"
OPENROUTER_IMAGE_MODEL="${OPENROUTER_IMAGE_MODEL:-google/gemini-2.5-flash-image}"
IMAGE_BASE_URL="${IMAGE_BASE_URL:-https://openrouter.ai/api/v1}"
SEEDREAM_API_KEY="${SEEDREAM_API_KEY:-}"
SEEDREAM_IMAGE_MODEL="${SEEDREAM_IMAGE_MODEL:-doubao-seedream-4-0-250828}"
SEEDREAM_BASE_URL="${SEEDREAM_BASE_URL:-https://ark.cn-beijing.volces.com/api/v3}"
NANO_ENABLE_PPROF="${NANO_ENABLE_PPROF:-false}"
NANO_PPROF_PORT="${NANO_PPROF_PORT:-0}"
NANO_RESPONSE_TIMEOUT="${NANO_RESPONSE_TIMEOUT:-600s}"
NANO_HTTP_TIMEOUT="${NANO_HTTP_TIMEOUT:-60s}"
NANO_CONTEXT_MAX_TOKENS="${NANO_CONTEXT_MAX_TOKENS:-80000}"
NANO_CONTEXT_COMPRESSION_RATIO="${NANO_CONTEXT_COMPRESSION_RATIO:-0.25}"
NANO_CONTEXT_PRESERVE_RECENT_TURNS="${NANO_CONTEXT_PRESERVE_RECENT_TURNS:-6}"
NANO_CONTEXT_ENABLE_COMPRESSION="${NANO_CONTEXT_ENABLE_COMPRESSION:-true}"
NANO_REASONING_ENABLED="${NANO_REASONING_ENABLED:-true}"
NANO_REASONING_EFFORT="${NANO_REASONING_EFFORT:-medium}"
NANO_REASONING_MAX_TOKENS="${NANO_REASONING_MAX_TOKENS:-0}"
NANO_REASONING_EXCLUDE="${NANO_REASONING_EXCLUDE:-false}"
NANO_WORKING_DIR="${NANO_WORKING_DIR:-/home/${EC2_USER}/nano-workspaces}"
NANO_ENABLED_TOOLS="${NANO_ENABLED_TOOLS:-}"
NANO_DISABLED_TOOLS="${NANO_DISABLED_TOOLS:-}"
NANO_ALLOWED_COMMANDS="${NANO_ALLOWED_COMMANDS:-}"
NANO_BLOCKED_COMMANDS="${NANO_BLOCKED_COMMANDS:-}"
NANO_ALLOWED_ENV_VARS="${NANO_ALLOWED_ENV_VARS:-}"
NANO_BLOCKED_ENV_VARS="${NANO_BLOCKED_ENV_VARS:-}"
NANO_STRICT="${NANO_STRICT:-false}"
NANO_ENABLE_MCP="${NANO_ENABLE_MCP:-true}"
NANO_MCP_DEFAULT_TRANSPORT="${NANO_MCP_DEFAULT_TRANSPORT:-stdio}"
NANO_MCP_TIMEOUT="${NANO_MCP_TIMEOUT:-180s}"
NANO_MCP_MAX_RETRIES="${NANO_MCP_MAX_RETRIES:-3}"
NANO_MCP_ENABLE_HEALTH_CHECK="${NANO_MCP_ENABLE_HEALTH_CHECK:-true}"
NANO_MCP_HEALTH_CHECK_INTERVAL="${NANO_MCP_HEALTH_CHECK_INTERVAL:-30s}"
NANO_MCP_HEALTH_CHECK_TIMEOUT="${NANO_MCP_HEALTH_CHECK_TIMEOUT:-10s}"
NANO_MCP_DEEPWIKI_AUTHORIZATION="${NANO_MCP_DEEPWIKI_AUTHORIZATION:-}"
NANO_MCP_GITHUB_TOKEN="${NANO_MCP_GITHUB_TOKEN:-}"
NANO_DAEMON_PID_FILE="${NANO_DAEMON_PID_FILE:-${DEPLOY_DIR}/.nano/daemon.pid}"
NANO_DAEMON_LOG_FILE="${NANO_DAEMON_LOG_FILE:-${DEPLOY_DIR}/logs/daemon.log}"
NANO_DAEMON_TLS_CERT_FILE="${NANO_DAEMON_TLS_CERT_FILE:-}"
NANO_DAEMON_TLS_KEY_FILE="${NANO_DAEMON_TLS_KEY_FILE:-}"
NANO_SANDBOX_ENABLED="${NANO_SANDBOX_ENABLED:-true}"
NANO_SANDBOX_NETWORK_ACCESS="${NANO_SANDBOX_NETWORK_ACCESS:-true}"
NANO_SANDBOX_ALLOWED_PATHS="${NANO_SANDBOX_ALLOWED_PATHS:-}"
NANO_SANDBOX_BLOCKED_PATHS="${NANO_SANDBOX_BLOCKED_PATHS:-/etc,/sys,/proc,/dev}"
NANO_SANDBOX_READ_ONLY_PATHS="${NANO_SANDBOX_READ_ONLY_PATHS:-}"
NANO_SANDBOX_EXTRA_READ_ONLY_PATHS="${NANO_SANDBOX_EXTRA_READ_ONLY_PATHS:-}"
NANO_SANDBOX_EXTRA_WRITABLE_PATHS="${NANO_SANDBOX_EXTRA_WRITABLE_PATHS:-}"
NANO_SANDBOX_BWRAP_PATH="${NANO_SANDBOX_BWRAP_PATH:-}"

# Expand possible ~ in PEM_FILE early so ssh/scp can find it
PEM_FILE=$(eval echo "$PEM_FILE")

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to display usage
show_usage() {
    echo -e "${BLUE}Nano Agent Unified Deployment Script${NC}"
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  deploy     - Full deployment (build, transfer, install, start)"
    echo "  build      - Build binary only"
    echo "  transfer   - Transfer files only"
    echo "  install    - Install and configure on EC2"
    echo "  start      - Start daemon services"
    echo "  stop       - Stop daemon services"
    echo "  restart    - Restart daemon services"
    echo "  status     - Check daemon status"
    echo "  test       - Run comprehensive health tests"
    echo "  monitor    - Start monitoring mode"
    echo "  logs       - Show daemon logs"
    echo "  help       - Show this help message"
    echo ""
}

# Function to log messages
log() {
    local level=$1
    shift
    local message="$@"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo -e "[$timestamp] [${level}] $message" >&2
}

# Render configuration locally using available tools (envsubst/python/go)
render_config_locally() {
    local template_path="deployment/daemon-config-template.yaml"
    local rendered_path="/tmp/nano-agent-config.yaml"

    if [ ! -f "$template_path" ]; then
        log "WARN" "${YELLOW}Config template not found at ${template_path}${NC}"
        return 1
    fi

    # Prefer envsubst if available
    if command -v envsubst >/dev/null 2>&1; then
        log "INFO" "${YELLOW}Rendering config via envsubst...${NC}"
        envsubst < "$template_path" > "$rendered_path"
    else
        # Fallback to python3 expandvars
        if command -v python3 >/dev/null 2>&1; then
            log "INFO" "${YELLOW}Rendering config via python3 expandvars...${NC}"
            python3 - <<'PY'
import os, sys
src = 'deployment/daemon-config-template.yaml'
dst = '/tmp/nano-agent-config.yaml'
with open(src, 'r', encoding='utf-8') as f:
    data = f.read()
expanded = os.path.expandvars(data)
with open(dst, 'w', encoding='utf-8') as f:
    f.write(expanded)
PY
        else
            # Fallback to inline Go (os.ExpandEnv)
            if command -v go >/dev/null 2>&1; then
                log "INFO" "${YELLOW}Rendering config via inline Go...${NC}"
                cat > /tmp/render_config.go <<'GO'
package main
import (
    "os"
    "fmt"
)
func main(){
    in := "deployment/daemon-config-template.yaml"
    out := "/tmp/nano-agent-config.yaml"
    b, err := os.ReadFile(in)
    if err != nil { fmt.Println("ERR:", err); os.Exit(1) }
    s := os.ExpandEnv(string(b))
    if err := os.WriteFile(out, []byte(s), 0644); err != nil { fmt.Println("ERR:", err); os.Exit(1) }
}
GO
                go run /tmp/render_config.go >/dev/null 2>&1 || {
                    log "ERROR" "${RED}Failed to render config via Go${NC}"
                    return 1
                }
            else
                log "ERROR" "${RED}No renderer available (envsubst/python3/go)${NC}"
                return 1
            fi
        fi
    fi

    # Basic sanity check: ensure file exists and non-empty
    if [ ! -s "$rendered_path" ]; then
        log "ERROR" "${RED}Rendered config is empty or missing${NC}"
        return 1
    fi
    echo "$rendered_path"
    return 0
}

# Detect remote EC2 architecture and set TARGET_ARCH accordingly
# Maps uname -m to Go arch: x86_64->amd64, aarch64/arm64->arm64
# Returns 0 on success
detect_remote_arch() {
    log "INFO" "${YELLOW}Detecting remote architecture on ${EC2_HOST}...${NC}"
    local arch_out
    arch_out=$(ssh -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=10 -i "$PEM_FILE" "$EC2_USER@$EC2_HOST" "uname -m" 2>/dev/null)
    if [ $? -ne 0 ] || [ -z "$arch_out" ]; then
        log "WARN" "${YELLOW}Could not detect remote architecture. Using default: ${TARGET_ARCH}${NC}"
        return 1
    fi
    case "$arch_out" in
        x86_64|amd64)
            TARGET_ARCH="amd64"
            ;;
        aarch64|arm64)
            TARGET_ARCH="arm64"
            ;;
        *)
            log "WARN" "${YELLOW}Unrecognized remote arch '$arch_out', defaulting to ${TARGET_ARCH}${NC}"
            return 1
            ;;
    esac
    log "INFO" "${GREEN}✓ Remote architecture: $arch_out -> GOARCH=${TARGET_ARCH}${NC}"
    return 0
}

# Function to build binary
build_binary() {
    # Try to detect and adjust TARGET_ARCH to match the EC2 instance to avoid Exec format error (203/EXEC)
    detect_remote_arch || true

    log "INFO" "${YELLOW}Building binary for ${TARGET_OS}/${TARGET_ARCH}...${NC}"
    cd $LOCAL_PROJECT_DIR

    # Check if we're in the right directory
    if [ ! -f "go.mod" ]; then
        log "ERROR" "${RED}Not in the nano-agent project directory${NC}"
        return 1
    fi

    mkdir -p bin

    # Cross-compile for Linux with CGO disabled to produce a portable static binary
    CGO_ENABLED=0 GOOS=$TARGET_OS GOARCH=$TARGET_ARCH go build \
        -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo 'dev') \
                  -X main.buildTime=$(date -u '+%Y-%m-%d_%H:%M:%S') \
                  -X main.commitHash=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" \
        -o bin/${BINARY_NAME}-${TARGET_OS}-${TARGET_ARCH} ./cmd/nano

    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to build binary${NC}"
        return 1
    fi

    log "SUCCESS" "${GREEN}✓ Binary compiled successfully: bin/${BINARY_NAME}-${TARGET_OS}-${TARGET_ARCH}${NC}"

    return 0
}

# Function to transfer files
transfer_files() {
    log "INFO" "${YELLOW}Transferring files to EC2...${NC}"

    # Create necessary directories on remote host first
    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST "mkdir -p ~/.config/nano ~/.nano $DEPLOY_DIR $DEPLOY_DIR/logs $DEPLOY_DIR/run && sudo chown -R $EC2_USER:$EC2_USER $DEPLOY_DIR"
    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to create directories on EC2${NC}"
        return 1
    fi

    # Remove existing binary before transfer
    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST "rm -f $DEPLOY_DIR/$BINARY_NAME"

    # Transfer binary to deployment directory
    scp -o StrictHostKeyChecking=no -i $PEM_FILE bin/${BINARY_NAME}-${TARGET_OS}-${TARGET_ARCH} $EC2_USER@$EC2_HOST:$DEPLOY_DIR/$BINARY_NAME
    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to transfer binary${NC}"
        return 1
    fi

    # Render configuration locally and transfer
    local rendered_cfg
    rendered_cfg=$(render_config_locally | tail -n 1) || {
        log "WARN" "${YELLOW}Rendering config failed; proceeding without config upload${NC}"
        rendered_cfg=""
    }
    if [ -n "$rendered_cfg" ]; then
        scp -o StrictHostKeyChecking=no -i $PEM_FILE "$rendered_cfg" $EC2_USER@$EC2_HOST:$DEPLOY_DIR/config.yaml
        if [ $? -ne 0 ]; then
            log "ERROR" "${RED}Failed to transfer rendered configuration${NC}"
            return 1
        fi
        rm -f "$rendered_cfg" 2>/dev/null || true
    fi

    # Transfer monitoring script
    scp -o StrictHostKeyChecking=no -i $PEM_FILE deployment/daemon-monitor.sh $EC2_USER@$EC2_HOST:$DEPLOY_DIR/
    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to transfer monitoring script${NC}"
        return 1
    fi

    log "SUCCESS" "${GREEN}✓ Files transferred successfully${NC}"
    return 0
}

# Function to install and configure on EC2
install_on_ec2() {
    log "INFO" "${YELLOW}Installing and configuring on EC2...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST <<EOF
        echo "Installing binary and configuration..."

        # Create necessary directories
        mkdir -p ~/.config/nano
        mkdir -p ~/.nano
        mkdir -p $DEPLOY_DIR/logs
        mkdir -p $DEPLOY_DIR/run

        # Create default working directory for agent tasks
        mkdir -p ${NANO_WORKING_DIR}
        echo "Working directory: ${NANO_WORKING_DIR}"

        # Fix DNS resolution issue by adding hostname to /etc/hosts
        echo "Fixing DNS resolution for hostname..."
        HOSTNAME=\$(hostname)
        if ! grep -q "127.0.0.1 \$HOSTNAME" /etc/hosts; then
            echo "127.0.0.1 \$HOSTNAME" | sudo tee -a /etc/hosts
            echo "Added \$HOSTNAME to /etc/hosts"
        else
            echo "Hostname \$HOSTNAME already in /etc/hosts"
        fi

        # Install the binary using symbolic links
        sudo chmod +x $DEPLOY_DIR/nano
        sudo rm -f /usr/local/bin/nano  # Remove existing link/file if any
        sudo ln -s $DEPLOY_DIR/nano /usr/local/bin/nano

        # Install the configuration if provided
        if [ -f $DEPLOY_DIR/config.yaml ]; then
            sudo rm -f ~/.config/nano/config.yaml
            ln -s $DEPLOY_DIR/config.yaml ~/.config/nano/config.yaml
        else
            echo "No config.yaml provided, using defaults"
        fi

        # Install the monitoring script using symbolic links
        sudo chmod +x $DEPLOY_DIR/daemon-monitor.sh
        sudo rm -f /usr/local/bin/daemon-monitor.sh  # Remove existing link/file if any
        sudo ln -s $DEPLOY_DIR/daemon-monitor.sh /usr/local/bin/daemon-monitor.sh

        # Install Node.js and npm if not present (for MCP servers)
        if ! command -v node &> /dev/null; then
            echo "Installing Node.js and npm..."
            sudo apt-get update -y
            curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -
            sudo apt-get install -y nodejs
        fi

        # Install bubblewrap (bwrap) if not present (required for sandbox on Linux)
        if ! command -v bwrap &> /dev/null; then
            echo "Installing bubblewrap (bwrap) for sandbox support..."
            sudo apt-get update -y
            sudo apt-get install -y bubblewrap
            echo "bubblewrap installed: $(bwrap --version 2>/dev/null || echo unknown)"
        else
            echo "bubblewrap already installed: $(bwrap --version 2>/dev/null || echo unknown)"
        fi

        # Create systemd service file with foreground mode (FIXED)
        sudo tee /etc/systemd/system/nano-daemon.service > /dev/null <<SERVICE_EOF
[Unit]
Description=Nano Agent Daemon
After=network.target
Wants=network.target

[Service]
Type=simple
User=${EC2_USER}
WorkingDirectory=${DEPLOY_DIR}
ExecStart=/usr/local/bin/nano daemon foreground
Restart=always
RestartSec=10
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=45
# Ensure all child processes (including Playwright) are cleaned up
ExecStopPost=/bin/bash -c 'pkill -f "npm exec @playw" || true; pkill -f "playwright" || true'

# Environment variables
Environment=HOME=/home/${EC2_USER}
Environment=USER=${EC2_USER}
# Daemon config via environment
Environment="NANO_DAEMON_HOST=${DAEMON_HOST_EXPORT}"
Environment="NANO_DAEMON_PORT=${DAEMON_PORT}"
Environment="NANO_DAEMON_API_KEY=${API_KEY}"
Environment="NANO_DAEMON_ENABLE_CORS=true"
Environment="NANO_API_KEY=${NANO_API_KEY}"
Environment="NANO_BASE_URL=${NANO_BASE_URL}"
Environment="NANO_MODEL=${NANO_MODEL}"
Environment="NANO_VERBOSE=${NANO_VERBOSE}"
Environment="SERPER_API_KEY=${SERPER_API_KEY}"
Environment="TAVILY_API_KEY=${TAVILY_API_KEY}"
Environment="OSS_ENABLED=${OSS_ENABLED}"
Environment="OSS_ACCESS_KEY_ID=${OSS_ACCESS_KEY_ID}"
Environment="OSS_ACCESS_KEY_SECRET=${OSS_ACCESS_KEY_SECRET}"
Environment="OSS_ENDPOINT=${OSS_ENDPOINT}"
Environment="OSS_DEFAULT_BUCKET=${OSS_DEFAULT_BUCKET}"
Environment="OSS_REGION=${OSS_REGION}"
Environment="OSS_TIMEOUT=${OSS_TIMEOUT}"
Environment="OSS_CALLBACK_URL=${OSS_CALLBACK_URL}"
Environment="OSS_CALLBACK_TOKEN=${OSS_CALLBACK_TOKEN}"
Environment="IMAGE_API_KEY=${IMAGE_API_KEY}"
Environment="IMAGE_BASE_URL=${IMAGE_BASE_URL}"
Environment="NANO_ENABLE_PPROF=${NANO_ENABLE_PPROF}"
Environment="OPENROUTER_IMAGE_MODEL=${OPENROUTER_IMAGE_MODEL}"
Environment="SEEDREAM_IMAGE_MODEL=${SEEDREAM_IMAGE_MODEL}"
Environment="NANO_PPROF_PORT=${NANO_PPROF_PORT}"
Environment="NANO_RESPONSE_TIMEOUT=${NANO_RESPONSE_TIMEOUT}"
Environment="NANO_HTTP_TIMEOUT=${NANO_HTTP_TIMEOUT}"
Environment="NANO_CONTEXT_MAX_TOKENS=${NANO_CONTEXT_MAX_TOKENS}"
Environment="NANO_CONTEXT_COMPRESSION_RATIO=${NANO_CONTEXT_COMPRESSION_RATIO}"
Environment="NANO_CONTEXT_PRESERVE_RECENT_TURNS=${NANO_CONTEXT_PRESERVE_RECENT_TURNS}"
Environment="NANO_CONTEXT_ENABLE_COMPRESSION=${NANO_CONTEXT_ENABLE_COMPRESSION}"
Environment="NANO_REASONING_ENABLED=${NANO_REASONING_ENABLED}"
Environment="NANO_REASONING_EFFORT=${NANO_REASONING_EFFORT}"
Environment="NANO_REASONING_MAX_TOKENS=${NANO_REASONING_MAX_TOKENS}"
Environment="NANO_REASONING_EXCLUDE=${NANO_REASONING_EXCLUDE}"
Environment="NANO_WORKING_DIR=${NANO_WORKING_DIR}"
Environment="NANO_ENABLED_TOOLS=${NANO_ENABLED_TOOLS}"
Environment="NANO_DISABLED_TOOLS=${NANO_DISABLED_TOOLS}"
Environment="NANO_ALLOWED_COMMANDS=${NANO_ALLOWED_COMMANDS}"
Environment="NANO_BLOCKED_COMMANDS=${NANO_BLOCKED_COMMANDS}"
Environment="NANO_ALLOWED_ENV_VARS=${NANO_ALLOWED_ENV_VARS}"
Environment="NANO_BLOCKED_ENV_VARS=${NANO_BLOCKED_ENV_VARS}"
Environment="NANO_STRICT=${NANO_STRICT}"
Environment="NANO_ENABLE_MCP=${NANO_ENABLE_MCP}"
Environment="NANO_MCP_DEFAULT_TRANSPORT=${NANO_MCP_DEFAULT_TRANSPORT}"
Environment="NANO_MCP_TIMEOUT=${NANO_MCP_TIMEOUT}"
Environment="NANO_MCP_MAX_RETRIES=${NANO_MCP_MAX_RETRIES}"
Environment="NANO_MCP_ENABLE_HEALTH_CHECK=${NANO_MCP_ENABLE_HEALTH_CHECK}"
Environment="NANO_MCP_HEALTH_CHECK_INTERVAL=${NANO_MCP_HEALTH_CHECK_INTERVAL}"
Environment="NANO_MCP_HEALTH_CHECK_TIMEOUT=${NANO_MCP_HEALTH_CHECK_TIMEOUT}"
Environment="NANO_DAEMON_LOG_FILE=${NANO_DAEMON_LOG_FILE}"
# Sandbox
Environment="NANO_SANDBOX_ENABLED=${NANO_SANDBOX_ENABLED}"
Environment="NANO_SANDBOX_NETWORK_ACCESS=${NANO_SANDBOX_NETWORK_ACCESS}"
Environment="NANO_SANDBOX_ALLOWED_PATHS=${NANO_SANDBOX_ALLOWED_PATHS}"
Environment="NANO_SANDBOX_BLOCKED_PATHS=${NANO_SANDBOX_BLOCKED_PATHS}"
Environment="NANO_SANDBOX_READ_ONLY_PATHS=${NANO_SANDBOX_READ_ONLY_PATHS}"
Environment="NANO_SANDBOX_EXTRA_READ_ONLY_PATHS=${NANO_SANDBOX_EXTRA_READ_ONLY_PATHS}"
Environment="NANO_SANDBOX_EXTRA_WRITABLE_PATHS=${NANO_SANDBOX_EXTRA_WRITABLE_PATHS}"
Environment="NANO_SANDBOX_BWRAP_PATH=${NANO_SANDBOX_BWRAP_PATH}"

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nano-daemon

[Install]
WantedBy=multi-user.target
SERVICE_EOF

        # Create sudoers rule for monitor to manage systemd services
        sudo tee /etc/sudoers.d/nano-daemon-monitor > /dev/null <<SUDOERS_EOF
# Allow ${EC2_USER} user to manage nano-daemon service without password
${EC2_USER} ALL=(ALL) NOPASSWD: /bin/systemctl start nano-daemon.service
${EC2_USER} ALL=(ALL) NOPASSWD: /bin/systemctl stop nano-daemon.service
${EC2_USER} ALL=(ALL) NOPASSWD: /bin/systemctl restart nano-daemon.service
${EC2_USER} ALL=(ALL) NOPASSWD: /bin/systemctl is-active nano-daemon.service
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/pkill -f "nano daemon"
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/pkill -f "npm exec @playw"
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/pkill -f "playwright"
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/pkill -f "daemon-monitor"
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/touch /var/log/nano-daemon-monitor.log
${EC2_USER} ALL=(ALL) NOPASSWD: /usr/bin/chown ${EC2_USER}\:${EC2_USER} /var/log/nano-daemon-monitor.log
SUDOERS_EOF

        # Create systemd service file for the monitor
        sudo tee /etc/systemd/system/nano-daemon-monitor.service > /dev/null <<MONITOR_EOF
[Unit]
Description=Nano Agent Daemon Health Monitor
After=network.target nano-daemon.service
Wants=network.target

[Service]
Type=simple
User=${EC2_USER}
WorkingDirectory=${DEPLOY_DIR}
ExecStart=/usr/local/bin/daemon-monitor.sh start
ExecStop=/usr/local/bin/daemon-monitor.sh stop
Restart=always
RestartSec=60
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=45
# Clean up any remaining processes
ExecStopPost=/bin/bash -c 'pkill -f "daemon-monitor" || true'

# Environment variables
Environment=HOME=/home/${EC2_USER}
Environment=USER=${EC2_USER}
# Monitor config via environment (match daemon)
Environment="DAEMON_HOST=localhost"
Environment="DAEMON_PORT=${DAEMON_PORT}"
Environment="DAEMON_API_KEY=${API_KEY}"

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nano-daemon-monitor

[Install]
WantedBy=multi-user.target
MONITOR_EOF

        # Reload systemd and enable services
        sudo systemctl daemon-reload
        sudo systemctl enable nano-daemon.service
        sudo systemctl enable nano-daemon-monitor.service

        echo "Installation completed successfully."
EOF

    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to install on EC2${NC}"
        return 1
    fi

    # Verify remote binary format to catch Exec format issues early
    log "INFO" "${YELLOW}Verifying remote binary format...${NC}"
    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST "file -b /usr/local/bin/nano || ls -l /usr/local/bin/nano"

    log "SUCCESS" "${GREEN}✓ Installation completed${NC}"
    return 0
}

# Function to start daemon services
start_services() {
    log "INFO" "${YELLOW}Starting daemon services...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST << EOF
        # Stop any existing processes
        sudo systemctl stop nano-daemon.service 2>/dev/null || true
        sudo systemctl stop nano-daemon-monitor.service 2>/dev/null || true

        # Kill any remaining processes
        pkill -f "nano daemon" 2>/dev/null || true

        # Wait for cleanup
        sleep 3

        # Start the daemon service
        echo "Starting nano daemon service..."
        sudo systemctl start nano-daemon.service

        # Wait for daemon to start
        sleep 5

        # Start the monitoring service
        echo "Starting nano daemon monitor..."
        sudo systemctl start nano-daemon-monitor.service

        echo "Services started successfully."
EOF

    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to start services${NC}"
        return 1
    fi

    log "SUCCESS" "${GREEN}✓ Services started successfully${NC}"
    return 0
}

# Function to stop daemon services
stop_services() {
    log "INFO" "${YELLOW}Stopping daemon services...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST << 'EOF'
        echo "Stopping nano daemon services..."
        sudo systemctl stop nano-daemon-monitor.service 2>/dev/null || true
        sudo systemctl stop nano-daemon.service 2>/dev/null || true

        # Force kill any remaining processes
        pkill -f "nano daemon" 2>/dev/null || true

        echo "Services stopped successfully."
EOF

    log "SUCCESS" "${GREEN}✓ Services stopped successfully${NC}"
    return 0
}

# Function to check daemon status
check_status() {
    log "INFO" "${YELLOW}Checking daemon status...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST << EOF
        echo "=== Daemon Service Status ==="
        sudo systemctl status nano-daemon.service --no-pager -l
        echo ""
        echo "=== Monitor Service Status ==="
        sudo systemctl status nano-daemon-monitor.service --no-pager -l
        echo ""
        echo "=== Process Status ==="
        ps aux | grep nano | grep -v grep || echo "No nano processes found"
        echo ""
EOF
}

# Function to run comprehensive tests
run_tests() {
    log "INFO" "${YELLOW}Running comprehensive health tests...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST << EOF
        echo "=== Daemon Health Tests ==="

        # Check if daemon process is running
        echo "Checking daemon process:"
        ps aux | grep nano | grep -v grep || echo "No nano processes found"
        echo ""

        # Test public health endpoint
        echo "Testing public health endpoint:"
        curl -v http://localhost:$DAEMON_PORT/health 2>&1 | head -10
        echo ""

        # Check daemon logs (may be empty when running in foreground)
        echo "Checking daemon logs:"
        tail -20 ${NANO_DAEMON_LOG_FILE} 2>/dev/null || tail -20 ~/.nano/daemon.log 2>/dev/null || echo "No daemon log file found"
        echo ""

        # Check systemd service status
        echo "Checking systemd service status:"
        sudo systemctl status nano-daemon.service --no-pager -l
EOF
}

# Function to show logs
show_logs() {
    log "INFO" "${YELLOW}Showing daemon logs...${NC}"

    ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST << EOF
        echo "=== Systemd Journal Logs ==="
        sudo journalctl -u nano-daemon.service -n 50 --no-pager
        echo ""
        echo "=== Daemon Log File ==="
        tail -50 ${NANO_DAEMON_LOG_FILE} 2>/dev/null || tail -50 ~/.nano/daemon.log 2>/dev/null || echo "No daemon log file found"
EOF
}





# Function for full deployment with integrated fixes
full_deploy() {
    log "INFO" "${BLUE}=== Starting Full Deployment with Integrated Fixes ===${NC}"

    build_binary || return 1
    transfer_files || return 1
    install_on_ec2 || return 1

    # Apply comprehensive fixes during deployment
    log "INFO" "${YELLOW}Applying system fixes during deployment...${NC}"

    ssh -i $PEM_FILE $EC2_USER@$EC2_HOST << 'DEPLOY_FIXES_EOF'
        echo "=== Applying Deployment Fixes ==="

        # Fix 1: DNS Resolution Issue
        echo "[1/4] Ensuring DNS resolution..."
        HOSTNAME=$(hostname)
        if ! grep -q "127.0.0.1 $HOSTNAME" /etc/hosts; then
            echo "127.0.0.1 $HOSTNAME" | sudo tee -a /etc/hosts
            echo "✓ Added $HOSTNAME to /etc/hosts"
        else
            echo "✓ Hostname $HOSTNAME already in /etc/hosts"
        fi

        # Fix 2: SystemD State Reset
        echo "[2/4] Resetting systemd state..."
        sudo systemctl daemon-reload
        sudo systemctl reset-failed nano-daemon.service 2>/dev/null || true
        sudo systemctl reset-failed nano-daemon-monitor.service 2>/dev/null || true
        echo "✓ SystemD state reset completed"

        # Fix 3: Clean up any existing processes
        echo "[3/4] Cleaning up existing processes..."
        sudo systemctl stop nano-daemon-monitor.service 2>/dev/null || true
        sudo systemctl stop nano-daemon.service 2>/dev/null || true

        # Comprehensive process cleanup (including Playwright)
        sudo pkill -f "nano daemon" 2>/dev/null || true
        sudo pkill -f "daemon-monitor" 2>/dev/null || true
        sudo pkill -f "npm exec @playw" 2>/dev/null || true
        sudo pkill -f "playwright" 2>/dev/null || true
        sudo pkill -f "node.*playwright" 2>/dev/null || true

        # Force kill any stubborn processes
        if pgrep -f "npm exec @playw\|playwright" > /dev/null 2>&1; then
            echo "Force killing stubborn Playwright processes..."
            sudo pkill -9 -f "npm exec @playw" 2>/dev/null || true
            sudo pkill -9 -f "playwright" 2>/dev/null || true
            sudo pkill -9 -f "node.*playwright" 2>/dev/null || true
            sleep 3
        fi

        echo "✓ Process cleanup completed"

        # Fix 4: Ensure monitoring script is executable and linked
        echo "[4/4] Updating monitoring script..."
        sudo chmod +x /opt/nano-agent/daemon-monitor.sh
        sudo rm -f /usr/local/bin/daemon-monitor.sh
        sudo ln -s /opt/nano-agent/daemon-monitor.sh /usr/local/bin/daemon-monitor.sh
        echo "✓ Monitoring script updated"

        echo "=== Deployment Fixes Applied Successfully ==="
DEPLOY_FIXES_EOF

    if [ $? -ne 0 ]; then
        log "ERROR" "${RED}Failed to apply deployment fixes${NC}"
        return 1
    fi

    start_services || return 1

    # Wait a moment for services to stabilize
    sleep 15

    # Run tests to verify deployment
    run_tests

    log "SUCCESS" "${GREEN}=== Deployment Complete with All Fixes Applied! ===${NC}"
    log "INFO" "${BLUE}Your nano agent daemon is now running with enhanced stability.${NC}"
    log "INFO" "${BLUE}Applied fixes include:${NC}"
    log "INFO" "${BLUE}  ✓ DNS resolution optimization${NC}"
    log "INFO" "${BLUE}  ✓ SystemD connection improvements${NC}"
    log "INFO" "${BLUE}  ✓ Playwright process management${NC}"
    log "INFO" "${BLUE}  ✓ Enhanced monitoring and error handling${NC}"
    log "INFO" "${BLUE}Access it at: http://$EC2_HOST:$DAEMON_PORT${NC}"
    log "INFO" "${BLUE}API key configured via environment for authenticated requests${NC}"
}

# Main script logic
case "${1:-help}" in
    "deploy")
        full_deploy
        ;;
    "build")
        build_binary
        ;;
    "transfer")
        transfer_files
        ;;
    "install")
        install_on_ec2
        ;;
    "start")
        start_services
        ;;
    "stop")
        stop_services
        ;;
    "restart")
        stop_services
        sleep 3
        start_services
        ;;
    "status")
        check_status
        ;;
    "test")
        run_tests
        ;;
    "logs")
        show_logs
        ;;


    "monitor")
        log "INFO" "Starting monitoring mode..."
        ssh -o StrictHostKeyChecking=no -i $PEM_FILE $EC2_USER@$EC2_HOST '/usr/local/bin/daemon-monitor.sh start'
        ;;
    "help")
        show_usage
        ;;
    *)
        echo -e "${RED}Unknown command: $1${NC}"
        show_usage
        exit 1
        ;;
esac

exit 0
