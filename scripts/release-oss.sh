#!/usr/bin/env bash
# Release script for uploading nano-agent binaries to Alibaba Cloud OSS
# This script uploads release artifacts to both versioned and latest paths

set -euo pipefail

# Get the current git tag
TAG="${TAG:-$(git describe --tags --exact-match HEAD 2>/dev/null || echo "")}"

if [ -z "$TAG" ]; then
    echo "Error: Not on a tagged commit. Please tag the commit first."
    echo "Usage: git tag v0.6.21 && git push origin v0.6.21"
    exit 1
fi

echo "Releasing nano-agent ${TAG}"

BUILD_DIR="${BUILD_DIR:-bin/release}"
BUCKET="${OSS_BUCKET:-oss://binary-releases}"

# Check if build directory exists
if [ ! -d "$BUILD_DIR" ]; then
    echo "Error: Build directory $BUILD_DIR does not exist"
    echo "Please run 'make release' first to build the release artifacts"
    exit 1
fi

# Check if ossutil is available
if ! command -v ossutil >/dev/null 2>&1; then
    echo "Error: ossutil is not installed or not in PATH"
    echo "Please install ossutil first: https://www.alibabacloud.com/help/en/oss/developer-reference/install-ossutil"
    exit 1
fi

# List of platforms to upload (excluding Windows for now)
PLATFORMS=(
    "darwin-amd64"
    "darwin-arm64"
    "linux-amd64"
    "linux-arm64"
)

echo "Uploading release artifacts..."

for platform in "${PLATFORMS[@]}"; do
    archive="${BUILD_DIR}/nano-${platform}.tar.gz"

    if [ ! -f "$archive" ]; then
        echo "Warning: Archive not found: $archive"
        echo "Skipping ${platform}..."
        continue
    fi

    echo "Uploading ${platform}..."

    # Upload to versioned path (e.g., oss://binary-releases/nano/v0.6.21/)
    versioned_path="${BUCKET}/nano/${TAG}/nano-${platform}.tar.gz"
    echo "  -> ${versioned_path}"
    ossutil cp "$archive" "$versioned_path" --force

    # Upload to latest path (e.g., oss://binary-releases/nano/latest/)
    latest_path="${BUCKET}/nano/latest/nano-${platform}.tar.gz"
    echo "  -> ${latest_path}"
    ossutil cp "$archive" "$latest_path" --force

    echo "  ✓ ${platform} uploaded"
done

echo ""
echo "✓ Release complete!"
echo ""
echo "Versioned URLs:"
for platform in "${PLATFORMS[@]}"; do
    echo "  https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/${TAG}/nano-${platform}.tar.gz"
done
echo ""
echo "Latest URLs:"
for platform in "${PLATFORMS[@]}"; do
    echo "  https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-${platform}.tar.gz"
done
