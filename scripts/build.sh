#!/bin/bash

# Build script for nano-agent

set -e

echo "Building nano..."

# Get version from git tag or use default
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS="-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.commitHash=${COMMIT_HASH}"

# Build for current platform
go build -ldflags "${LDFLAGS}" -o bin/nano ./cmd/nano

echo "Build completed: bin/nano"
echo "Version: ${VERSION}"
echo "Build time: ${BUILD_TIME}"
echo "Commit: ${COMMIT_HASH}"
