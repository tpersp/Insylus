#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  echo "usage: $0 <version>" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DIST_DIR="${INSYLUS_DIST_DIR:-$REPO_ROOT/dist}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

if [[ "$GOOS" == "linux" && "$GOARCH" == "arm" ]]; then
  BUNDLE_NAME="insylus-update-linux-armv7.tar.gz"
else
  BUNDLE_NAME="insylus-update-${GOOS}-${GOARCH}.tar.gz"
fi
CHECKSUM_NAME="${BUNDLE_NAME}-v${VERSION#v}.sha256"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

copy_required() {
  local src="$1"
  local dst="$2"
  if [[ ! -f "$DIST_DIR/$src" ]]; then
    echo "missing required release file: $DIST_DIR/$src" >&2
    exit 1
  fi
  install -m 0755 "$DIST_DIR/$src" "$TMP_DIR/$dst"
}

copy_required "insylus-server-${GOOS}-${GOARCH}" "insylus-server"
copy_required "insylusctl" "insylusctl"
copy_required "insylus-agent" "insylus-agent"
copy_required "insylus-agent-linux-amd64" "insylus-agent-linux-amd64"
copy_required "insylus-agent-linux-arm64" "insylus-agent-linux-arm64"
copy_required "insylus-agent-linux-armv7" "insylus-agent-linux-armv7"

tar -C "$TMP_DIR" -czf "$DIST_DIR/$BUNDLE_NAME" .
sha256sum "$DIST_DIR/$BUNDLE_NAME" | awk -v file="$BUNDLE_NAME" '{print $1 "  " file}' > "$DIST_DIR/$CHECKSUM_NAME"

echo "created $DIST_DIR/$BUNDLE_NAME"
echo "created $DIST_DIR/$CHECKSUM_NAME"
