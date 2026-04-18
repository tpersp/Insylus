#!/usr/bin/env bash
set -euo pipefail

APP_USER="${INSYLUS_APP_USER:-insylus}"
APP_GROUP="${INSYLUS_APP_GROUP:-insylus}"
MANAGED_USER="${INSYLUS_MANAGED_USER:-insylusmgr}"
MANAGED_GROUPS="${INSYLUS_MANAGED_GROUPS:-adm,systemd-journal}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_ROOT="${INSYLUS_INSTALL_ROOT:-/opt/insylus}"
BIN_DIR="${INSYLUS_BIN_DIR:-$INSTALL_ROOT/bin}"
DATA_DIR="${INSYLUS_DATA_DIR:-/var/lib/insylus}"
LISTEN_ADDR="${INSYLUS_LISTEN_ADDR:-:8080}"
SYSTEMD_DIR="${INSYLUS_SYSTEMD_DIR:-/etc/systemd/system}"
COMMAND_DIR="${INSYLUS_COMMAND_DIR:-/usr/local/bin}"
SERVER_BIN_SRC="${INSYLUS_SERVER_BIN_SRC:-$REPO_ROOT/dist/insylus-server}"
AGENT_BIN_SRC="${INSYLUS_AGENT_BIN_SRC:-$REPO_ROOT/dist/insylus-agent}"
CTL_BIN_SRC="${INSYLUS_CTL_BIN_SRC:-$REPO_ROOT/dist/insylusctl}"
SERVER_BIN_DST="$BIN_DIR/insylus-server"
AGENT_BIN_DST="$BIN_DIR/insylus-agent"
CTL_BIN_DST="$BIN_DIR/insylusctl"
INSYLUS_CTL_LINK_DST="${INSYLUS_CTL_LINK_DST:-$COMMAND_DIR/insylusctl}"
INSYLUS_LINK_DST="${INSYLUS_LINK_DST:-$COMMAND_DIR/insylus}"
UNIT_NAME="${INSYLUS_SERVICE_NAME:-insylus.service}"
UNIT_PATH="$SYSTEMD_DIR/$UNIT_NAME"

DEV_OWNER="$(stat -c '%U' "$REPO_ROOT")"
DEV_GROUP="$(stat -c '%G' "$REPO_ROOT")"

if [[ "$DEV_OWNER" == "UNKNOWN" || "$DEV_OWNER" == "root" ]]; then
  DEV_OWNER="${SUDO_USER:-root}"
fi

if [[ "$DEV_GROUP" == "UNKNOWN" || "$DEV_GROUP" == "root" ]]; then
  if getent group appgroup >/dev/null 2>&1; then
    DEV_GROUP="appgroup"
  elif [[ "$DEV_OWNER" != "root" ]]; then
    DEV_GROUP="$(id -gn "$DEV_OWNER")"
  else
    DEV_GROUP="root"
  fi
fi

if [[ ! -x "$SERVER_BIN_SRC" ]]; then
  echo "missing server binary at $SERVER_BIN_SRC" >&2
  exit 1
fi

if [[ ! -x "$AGENT_BIN_SRC" ]]; then
  echo "missing agent binary at $AGENT_BIN_SRC" >&2
  exit 1
fi

if [[ ! -x "$CTL_BIN_SRC" ]]; then
  echo "missing cli binary at $CTL_BIN_SRC" >&2
  exit 1
fi

if ! getent group "$APP_GROUP" >/dev/null 2>&1; then
  groupadd --system "$APP_GROUP"
fi

if ! id "$APP_USER" >/dev/null 2>&1; then
  useradd --system --gid "$APP_GROUP" --home-dir "$DATA_DIR" --create-home --shell /usr/sbin/nologin "$APP_USER"
fi

install -d -o "$DEV_OWNER" -g "$DEV_GROUP" -m 2775 "$INSTALL_ROOT"
install -d -o "$DEV_OWNER" -g "$DEV_GROUP" -m 2775 "$BIN_DIR"
install -d -o "$APP_USER" -g "$APP_GROUP" -m 0750 "$DATA_DIR"

install -o "$DEV_OWNER" -g "$DEV_GROUP" -m 0755 "$SERVER_BIN_SRC" "$SERVER_BIN_DST"
install -o "$DEV_OWNER" -g "$DEV_GROUP" -m 0755 "$AGENT_BIN_SRC" "$AGENT_BIN_DST"
install -o "$DEV_OWNER" -g "$DEV_GROUP" -m 0755 "$CTL_BIN_SRC" "$CTL_BIN_DST"
ln -sf "$CTL_BIN_DST" "$INSYLUS_CTL_LINK_DST"
ln -sf "$CTL_BIN_DST" "$INSYLUS_LINK_DST"

for extra_agent in "$REPO_ROOT"/dist/insylus-agent-linux-*; do
  if [[ -f "$extra_agent" ]]; then
    install -o "$DEV_OWNER" -g "$DEV_GROUP" -m 0755 "$extra_agent" "$BIN_DIR/$(basename "$extra_agent")"
  fi
done

cat >"$UNIT_PATH" <<EOF
[Unit]
Description=Insylus Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$APP_USER
Group=$APP_GROUP
WorkingDirectory=$DATA_DIR
ExecStart=$SERVER_BIN_DST -listen $LISTEN_ADDR -db $DATA_DIR/insylus.db -agent-binary $AGENT_BIN_DST -managed-user $MANAGED_USER -managed-groups $MANAGED_GROUPS
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "$UNIT_NAME"
systemctl status --no-pager "$UNIT_NAME"

if [[ -x "$REPO_ROOT/scripts/install-insylus-ssh-sync.sh" ]]; then
  INSYLUS_BIN_DIR="$BIN_DIR" \
    INSYLUS_DATA_DIR="$DATA_DIR" \
    INSYLUS_SYSTEMD_DIR="$SYSTEMD_DIR" \
    INSYLUS_SERVICE_NAME="$UNIT_NAME" \
    INSYLUS_MANAGED_USER="$MANAGED_USER" \
    "$REPO_ROOT/scripts/install-insylus-ssh-sync.sh"
fi
