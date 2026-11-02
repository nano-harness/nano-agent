#!/bin/bash

# Package skill documents for release
# Creates a tarball containing all skill documentation

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SKILLS_DIR="${PROJECT_ROOT}/docs/skills"
OUTPUT_DIR="${PROJECT_ROOT}/dist"
VERSION="${VERSION:-dev}"

echo "Packaging skill documents..."
echo "Version: ${VERSION}"
echo "Skills directory: ${SKILLS_DIR}"

# Create output directory if it doesn't exist
mkdir -p "${OUTPUT_DIR}"

# Package name
PACKAGE_NAME="nano-skills-${VERSION}.tar.gz"
PACKAGE_PATH="${OUTPUT_DIR}/${PACKAGE_NAME}"

# Create tarball
cd "${PROJECT_ROOT}"
tar -czf "${PACKAGE_PATH}" \
  -C docs \
  skills/

echo "Skill package created: ${PACKAGE_PATH}"

# Create latest symlink name (without version for consistency)
LATEST_PACKAGE_NAME="nano-skills.tar.gz"
LATEST_PACKAGE_PATH="${OUTPUT_DIR}/${LATEST_PACKAGE_NAME}"

# Copy to latest (for OSS latest directory)
cp "${PACKAGE_PATH}" "${LATEST_PACKAGE_PATH}"
echo "Latest package created: ${LATEST_PACKAGE_PATH}"

# Generate SHA256 checksum
sha256sum "${PACKAGE_PATH}" | awk '{print $1}' > "${PACKAGE_PATH}.sha256"
sha256sum "${LATEST_PACKAGE_PATH}" | awk '{print $1}' > "${LATEST_PACKAGE_PATH}.sha256"

echo "Checksums generated"
echo ""
echo "Package contents:"
tar -tzf "${PACKAGE_PATH}" | head -20

# Show package info
PACKAGE_SIZE=$(du -h "${PACKAGE_PATH}" | cut -f1)
echo ""
echo "Package size: ${PACKAGE_SIZE}"
echo "Package ready for release: ${PACKAGE_NAME}"
