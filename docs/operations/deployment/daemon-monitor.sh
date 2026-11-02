#!/bin/bash

# Nano Agent Daemon Health Monitor Script
# This script provides advanced monitoring and recovery capabilities
# beyond what systemd provides

# Configuration
DAEMON_PORT="${DAEMON_PORT:-8080}"
DAEMON_HOST="${DAEMON_HOST:-localhost}"
# Optional API key for daemon auth (matches server's X-API-Key)
DAEMON_API_KEY="${DAEMON_API_KEY:-}"
HEALTH_CHECK_INTERVAL=60  # seconds - increased to reduce false positives
MAX_RESTART_ATTEMPTS=3
RESTART_COOLDOWN=300     # seconds - increased cooldown to 5 minutes
HEALTH_CHECK_RETRIES=3   # number of failed health checks before restart
CONSECUTIVE_FAILURES=0   # track consecutive failures
LOG_FILE="/var/log/nano-daemon-monitor.log"
PID_FILE="/tmp/nano-daemon-monitor.pid"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging function
log() {
    local level=$1
    shift
    local message="$@"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] [$level] $message" | tee -a "$LOG_FILE"
}

# Check if daemon is responding to HTTP requests
check_daemon_health() {
    local health_url="http://${DAEMON_HOST}:${DAEMON_PORT}/health"

    # First check if the port is listening
    if ! netstat -ln 2>/dev/null | grep -q ":${DAEMON_PORT} " && ! ss -ln 2>/dev/null | grep -q ":${DAEMON_PORT} "; then
        log "DEBUG" "Port ${DAEMON_PORT} is not listening"
        return 1
    fi

    # Try to connect to the health endpoint
    local response
    response=$(curl -f -s --connect-timeout 5 --max-time 10 "$health_url" 2>&1)
    local curl_exit_code=$?

    if [ $curl_exit_code -eq 0 ]; then
        log "DEBUG" "Health check passed"
        return 0  # Healthy
    else
        log "DEBUG" "Health check failed with exit code $curl_exit_code: $response"
        return 1  # Unhealthy
    fi
}

# Check if systemd service is running with enhanced error handling
check_systemd_service() {
    local output
    local exit_code

    # Try to check service status with timeout and error handling
    output=$(timeout 30 sudo systemctl is-active nano-daemon.service 2>&1)
    exit_code=$?

    # Handle specific systemd connection errors
    if echo "$output" | grep -q "Transport endpoint is not connected\|Connection timed out\|Failed to get load state"; then
        log "WARN" "SystemD connection issue detected: $output"
        # Try to reset systemd state
        sudo systemctl daemon-reload 2>/dev/null || true
        sudo systemctl reset-failed nano-daemon.service 2>/dev/null || true
        sleep 2
        # Retry once
        output=$(timeout 15 sudo systemctl is-active nano-daemon.service 2>&1)
        exit_code=$?
    fi

    if [ $exit_code -eq 0 ] && [ "$output" = "active" ]; then
        return 0
    else
        log "DEBUG" "SystemD service check failed: exit_code=$exit_code, output=$output"
        return 1
    fi
}

# Restart the daemon via systemd with enhanced error handling
restart_daemon() {
    log "WARN" "Attempting to restart nano daemon via systemd"

    # Check and fix DNS resolution first
    local hostname=$(hostname)
    if ! grep -q "127.0.0.1 $hostname" /etc/hosts; then
        log "INFO" "Fixing DNS resolution for hostname $hostname"
        echo "127.0.0.1 $hostname" | sudo tee -a /etc/hosts >/dev/null
    fi

    # Reset systemd state to handle connection issues
    log "INFO" "Resetting systemd state"
    sudo systemctl daemon-reload 2>/dev/null || true
    sudo systemctl reset-failed nano-daemon.service 2>/dev/null || true

    # Stop the service with timeout
    log "INFO" "Stopping nano-daemon service"
    timeout 60 sudo systemctl stop nano-daemon.service 2>/dev/null || true
    sleep 3

    # Kill any remaining processes using the dedicated cleanup function
    cleanup_all_processes

    # Start the service with timeout
    log "INFO" "Starting nano-daemon service"
    if timeout 60 sudo systemctl start nano-daemon.service; then
        log "INFO" "Service start command completed"
    else
        log "WARN" "Service start command timed out or failed"
    fi

    # Wait for startup with progressive checks
    local wait_time=0
    local max_wait=30
    while [ $wait_time -lt $max_wait ]; do
        sleep 2
        wait_time=$((wait_time + 2))
        if check_systemd_service; then
            log "INFO" "Service is active after ${wait_time}s"
            break
        fi
    done

    # Final health check
    sleep 5
    if check_systemd_service && check_daemon_health; then
        log "INFO" "Daemon successfully restarted"
        return 0
    else
        log "ERROR" "Failed to restart daemon - service or health check failed"
        return 1
    fi
}

# Send alert (can be extended to send emails, Slack notifications, etc.)
send_alert() {
    local message="$1"
    log "ALERT" "$message"

    # You can extend this function to send notifications
    # For example:
    # curl -X POST -H 'Content-type: application/json' \
    #   --data "{\"text\":\"$message\"}" \
    #   YOUR_SLACK_WEBHOOK_URL
}

# Function to clean up all related processes
cleanup_all_processes() {
    log "INFO" "Performing comprehensive process cleanup..."

    # Clean up main daemon processes
    sudo pkill -f "nano daemon" 2>/dev/null || true

    # Clean up Playwright processes (common cause of leftover processes)
    sudo pkill -f "npm exec @playw" 2>/dev/null || true
    sudo pkill -f "playwright" 2>/dev/null || true

    # Clean up any Node.js processes that might be related
    sudo pkill -f "node.*playwright" 2>/dev/null || true

    sleep 3

    # Force kill stubborn processes
    if pgrep -f "npm exec @playw\|playwright" > /dev/null 2>&1; then
        log "WARN" "Force killing stubborn processes..."
        sudo pkill -9 -f "npm exec @playw" 2>/dev/null || true
        sudo pkill -9 -f "playwright" 2>/dev/null || true
        sudo pkill -9 -f "node.*playwright" 2>/dev/null || true
        sleep 2
    fi

    log "INFO" "Process cleanup completed"
}

# Main monitoring loop
monitor_daemon() {
    local restart_count=0
    local last_restart_time=0
    local consecutive_failures=0

    log "INFO" "Starting nano daemon monitor (PID: $$)"
    log "INFO" "Monitor configuration: check_interval=${HEALTH_CHECK_INTERVAL}s, max_restarts=${MAX_RESTART_ATTEMPTS}, cooldown=${RESTART_COOLDOWN}s"

    while true; do
        local current_time=$(date +%s)

        # Check if systemd service is running
        if ! check_systemd_service; then
            log "WARN" "Systemd service is not running"
            consecutive_failures=$((consecutive_failures + 1))

            # Reset restart count if enough time has passed
            if [ $((current_time - last_restart_time)) -gt $RESTART_COOLDOWN ]; then
                restart_count=0
            fi

            if [ $restart_count -lt $MAX_RESTART_ATTEMPTS ]; then
                restart_count=$((restart_count + 1))
                last_restart_time=$current_time

                log "INFO" "Attempting restart $restart_count/$MAX_RESTART_ATTEMPTS due to systemd service down"
                if restart_daemon; then
                    send_alert "Nano daemon was down, successfully restarted (attempt $restart_count/$MAX_RESTART_ATTEMPTS)"
                    consecutive_failures=0
                else
                    send_alert "Failed to restart nano daemon (attempt $restart_count/$MAX_RESTART_ATTEMPTS)"
                fi
            else
                send_alert "Nano daemon restart attempts exhausted. Manual intervention required."
                log "ERROR" "Maximum restart attempts reached. Waiting for cooldown period."
                sleep $RESTART_COOLDOWN
                restart_count=0
            fi
        # Check if daemon is responding to health checks
        elif ! check_daemon_health; then
            consecutive_failures=$((consecutive_failures + 1))
            log "WARN" "Daemon health check failed (consecutive failures: $consecutive_failures)"

            # Only restart after multiple consecutive failures to avoid false positives
            if [ $consecutive_failures -ge $HEALTH_CHECK_RETRIES ]; then
                # Reset restart count if enough time has passed
                if [ $((current_time - last_restart_time)) -gt $RESTART_COOLDOWN ]; then
                    restart_count=0
                fi

                if [ $restart_count -lt $MAX_RESTART_ATTEMPTS ]; then
                    restart_count=$((restart_count + 1))
                    last_restart_time=$current_time

                    log "INFO" "Attempting restart $restart_count/$MAX_RESTART_ATTEMPTS due to health check failures"
                    if restart_daemon; then
                        send_alert "Nano daemon health check failed, successfully restarted (attempt $restart_count/$MAX_RESTART_ATTEMPTS)"
                        consecutive_failures=0
                    else
                        send_alert "Failed to restart unresponsive nano daemon (attempt $restart_count/$MAX_RESTART_ATTEMPTS)"
                    fi
                else
                    send_alert "Nano daemon health check restart attempts exhausted. Manual intervention required."
                    log "ERROR" "Maximum restart attempts reached for health check failures. Waiting for cooldown period."
                    sleep $RESTART_COOLDOWN
                    restart_count=0
                    consecutive_failures=0
                fi
            fi
        else
            # Daemon is healthy
            if [ $consecutive_failures -gt 0 ]; then
                log "INFO" "Daemon is healthy again (was failing for $consecutive_failures checks)"
                consecutive_failures=0
            fi
            if [ $restart_count -gt 0 ]; then
                log "INFO" "Daemon is stable after restart"
            fi
        fi

        sleep $HEALTH_CHECK_INTERVAL
    done
}

# Signal handlers
cleanup() {
    log "INFO" "Stopping nano daemon monitor"
    rm -f "$PID_FILE"
    exit 0
}

trap cleanup SIGTERM SIGINT

# Command line interface
case "${1:-start}" in
    start)
        # Check if already running
        if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
            echo "Monitor is already running (PID: $(cat "$PID_FILE"))"
            exit 1
        fi

        # Create log file if it doesn't exist
        sudo touch "$LOG_FILE"
        sudo chown ubuntu:ubuntu "$LOG_FILE"

        # Start monitoring in background
        echo $$ > "$PID_FILE"
        monitor_daemon
        ;;
    stop)
        if [ -f "$PID_FILE" ]; then
            pid=$(cat "$PID_FILE")
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid"
                echo "Monitor stopped (PID: $pid)"
            else
                echo "Monitor was not running"
                rm -f "$PID_FILE"
            fi
        else
            echo "Monitor is not running"
        fi
        ;;
    status)
        if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
            echo "Monitor is running (PID: $(cat "$PID_FILE"))"
            echo "Log file: $LOG_FILE"
        else
            echo "Monitor is not running"
        fi
        ;;
    logs)
        if [ -f "$LOG_FILE" ]; then
            tail -f "$LOG_FILE"
        else
            echo "Log file not found: $LOG_FILE"
        fi
        ;;
    test)
        echo "Testing daemon health..."
        if check_daemon_health; then
            echo -e "${GREEN}✓ Daemon is healthy${NC}"
        else
            echo -e "${RED}✗ Daemon health check failed${NC}"
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|status|logs|test}"
        exit 1
        ;;
 esac
