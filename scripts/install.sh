#!/usr/bin/env bash
# install.sh - nano-agent CLI installer
# Usage:
#   curl -sSL https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/install.sh | bash

set -euo pipefail

BINARY_NAME="nano"
OSS_BASE_URL="https://binary-releases.oss-cn-hangzhou.aliyuncs.com"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"

# Determine OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${ARCH}" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

case "${OS}" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: ${OS}" >&2
    exit 1
    ;;
esac

ARTIFACT_NAME="${BINARY_NAME}-${OS}-${ARCH}"

# Resolve download URL from OSS
DOWNLOAD_URL="${OSS_BASE_URL}/nano/latest/${ARTIFACT_NAME}"

echo "Downloading ${ARTIFACT_NAME} ..."
echo "  from: ${DOWNLOAD_URL}"

TMP_FILE="$(mktemp)"
trap 'rm -f "${TMP_FILE}"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_FILE}"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${TMP_FILE}" "${DOWNLOAD_URL}"
else
  echo "Neither curl nor wget found. Please install one of them." >&2
  exit 1
fi

chmod +x "${TMP_FILE}"

# Ensure install directory exists and install without sudo.
mkdir -p "${INSTALL_DIR}"
mv "${TMP_FILE}" "${INSTALL_DIR}/${BINARY_NAME}"

echo ""
echo "nano installed successfully to ${INSTALL_DIR}/${BINARY_NAME}"
echo ""
echo "Make sure ${INSTALL_DIR} is in your PATH."
echo "  Add this line to your ~/.bashrc or ~/.zshrc:"
echo "    export PATH=\"\${HOME}/.local/bin:\${PATH}\""
echo ""
echo "Run 'nano --help' to get started."
