#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-}"
SERVICE_NAME="${INSYLUS_SERVICE_NAME:-insylus.service}"
TARGET_BINARY="${INSYLUS_SERVER_BINARY:-/opt/insylus/bin/insylus-server}"
TARGET_DIR="$(dirname "$TARGET_BINARY")"

if [[ "$EUID" -ne 0 ]]; then
  echo "apply-server-update must run as root" >&2
  exit 1
fi

if [[ "$ACTION" == "restart" ]]; then
  systemctl restart --no-block "$SERVICE_NAME"
  exit 0
fi

restore_backup() {
  local target_path="$1"
  local backup_path="${target_path}.backup"
  if [[ -f "$backup_path" ]]; then
    cp -f "$backup_path" "$target_path"
    chmod 0755 "$target_path"
  fi
}

if [[ "$ACTION" == "rollback" ]]; then
  for name in \
    insylus-server \
    insylusctl \
    insylus-agent \
    insylus-agent-linux-amd64 \
    insylus-agent-linux-arm64 \
    insylus-agent-linux-armv7; do
    restore_backup "$TARGET_DIR/$name"
  done
  exit 0
fi

if [[ "$ACTION" == "apply" ]]; then
  STAGED_PATH="${2:-}"
else
  STAGED_PATH="$ACTION"
fi

if [[ -z "$STAGED_PATH" || ! -e "$STAGED_PATH" ]]; then
  echo "staged update path is required" >&2
  exit 1
fi

if [[ ! -d "$TARGET_DIR" ]]; then
  echo "target directory does not exist: $TARGET_DIR" >&2
  exit 1
fi

install_target() {
  local source_path="$1"
  local target_path="$2"

  if [[ ! -f "$source_path" ]]; then
    echo "missing staged file: $source_path" >&2
    exit 1
  fi
  if [[ ! -x "$source_path" ]]; then
    echo "staged file must be executable: $source_path" >&2
    exit 1
  fi

  local owner="root"
  local group="root"
  if [[ -e "$target_path" ]]; then
    owner="$(stat -c '%U' "$target_path")"
    group="$(stat -c '%G' "$target_path")"
    cp -f "$target_path" "$target_path.backup"
    chown "$owner:$group" "$target_path.backup"
    chmod 0755 "$target_path.backup"
  fi

  local tmp_target
  tmp_target="$(mktemp "$TARGET_DIR/.$(basename "$target_path").XXXXXX")"
  cleanup_tmp() {
    rm -f "$tmp_target"
  }
  trap cleanup_tmp RETURN
  install -o "$owner" -g "$group" -m 0755 "$source_path" "$tmp_target"
  mv -f "$tmp_target" "$target_path"
  trap - RETURN
}

if [[ -d "$STAGED_PATH" ]]; then
  required=(
    insylus-server
    insylusctl
    insylus-agent
    insylus-agent-linux-amd64
    insylus-agent-linux-arm64
    insylus-agent-linux-armv7
  )
  for name in "${required[@]}"; do
    install_target "$STAGED_PATH/$name" "$TARGET_DIR/$name"
  done
  exit 0
fi

install_target "$STAGED_PATH" "$TARGET_BINARY"
