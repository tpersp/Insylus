#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-}"
SERVICE_NAME="${INSYLUS_SERVICE_NAME:-insylus.service}"
TARGET_BINARY="${INSYLUS_SERVER_BINARY:-/opt/insylus/bin/insylus-server}"

if [[ "$EUID" -ne 0 ]]; then
  echo "apply-server-update must run as root" >&2
  exit 1
fi

if [[ "$ACTION" == "restart" ]]; then
  systemctl restart --no-block "$SERVICE_NAME"
  exit 0
fi

if [[ "$ACTION" == "apply" ]]; then
  STAGED_BINARY="${2:-}"
else
  STAGED_BINARY="$ACTION"
fi

if [[ -z "$STAGED_BINARY" || ! -f "$STAGED_BINARY" ]]; then
  echo "staged server binary is required" >&2
  exit 1
fi

if [[ ! -x "$STAGED_BINARY" ]]; then
  echo "staged server binary must be executable" >&2
  exit 1
fi

TARGET_DIR="$(dirname "$TARGET_BINARY")"
if [[ ! -d "$TARGET_DIR" ]]; then
  echo "target directory does not exist: $TARGET_DIR" >&2
  exit 1
fi

OWNER="root"
GROUP="root"
if [[ -e "$TARGET_BINARY" ]]; then
  OWNER="$(stat -c '%U' "$TARGET_BINARY")"
  GROUP="$(stat -c '%G' "$TARGET_BINARY")"
  cp -f "$TARGET_BINARY" "$TARGET_BINARY.backup"
  chown "$OWNER:$GROUP" "$TARGET_BINARY.backup"
  chmod 0755 "$TARGET_BINARY.backup"
fi

TMP_TARGET="$(mktemp "$TARGET_DIR/.insylus-server.XXXXXX")"
cleanup() {
  rm -f "$TMP_TARGET"
}
trap cleanup EXIT

install -o "$OWNER" -g "$GROUP" -m 0755 "$STAGED_BINARY" "$TMP_TARGET"
mv -f "$TMP_TARGET" "$TARGET_BINARY"
trap - EXIT
