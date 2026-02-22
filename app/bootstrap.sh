#!/usr/bin/env bash
# bootstrap.sh - Import Alpine Linux as a base image into lildocker's image store
# Run this once before building your first image.
#
# Usage:
#   sudo ./bootstrap.sh                    # imports alpine:latest
#   sudo ./bootstrap.sh ubuntu:22.04       # imports ubuntu (if tarball available)

set -euo pipefail

STORE_ROOT="/var/lib/lildocker"
IMAGE_NAME="${1:-alpine}"
IMAGE_TAG="${2:-latest}"
ALPINE_VERSION="3.19.1"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-${ALPINE_VERSION}-x86_64.tar.gz"

if [[ $EUID -ne 0 ]]; then
  echo "❌ This script must be run as root (sudo ./bootstrap.sh)"
  exit 1
fi

echo "🚀 Bootstrapping ${IMAGE_NAME}:${IMAGE_TAG} into lildocker image store"

# Create directory structure
IMAGE_ID=$(echo -n "${IMAGE_NAME}:${IMAGE_TAG}:bootstrap" | sha256sum | cut -c1-32)
IMAGE_DIR="${STORE_ROOT}/images/${IMAGE_ID}"
LAYER_ID="base0000"
LAYER_DIR="${IMAGE_DIR}/layers/${LAYER_ID}"

mkdir -p "${LAYER_DIR}"

# Download and extract Alpine rootfs
TMPFILE=$(mktemp /tmp/lildocker-bootstrap-XXXXXX.tar.gz)
echo "📥 Downloading Alpine Linux ${ALPINE_VERSION} minirootfs..."
curl -fsSL "${ALPINE_URL}" -o "${TMPFILE}"

echo "📦 Extracting rootfs into layer ${LAYER_ID}..."
tar -xzf "${TMPFILE}" -C "${LAYER_DIR}"
rm -f "${TMPFILE}"

# Calculate size
SIZE=$(du -sb "${LAYER_DIR}" | cut -f1)

# Write manifest.json
cat > "${IMAGE_DIR}/manifest.json" << EOF
{
  "id": "${IMAGE_ID}",
  "name": "${IMAGE_NAME}",
  "tag": "${IMAGE_TAG}",
  "layers": ["${LAYER_ID}"],
  "env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],
  "cmd": ["/bin/sh"],
  "workdir": "/",
  "exposed_ports": [],
  "labels": {"bootstrapped": "true"},
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "size": ${SIZE}
}
EOF

echo ""
echo "✅ Bootstrapped ${IMAGE_NAME}:${IMAGE_TAG}"
echo "   Image ID : ${IMAGE_ID}"
echo "   Layer    : ${LAYER_DIR}"
echo "   Size     : $(du -sh ${LAYER_DIR} | cut -f1)"
echo ""
echo "You can now run:"
echo "  sudo ./lildocker build -t myapp:latest ./example"
echo "  sudo ./lildocker run myapp:latest"
