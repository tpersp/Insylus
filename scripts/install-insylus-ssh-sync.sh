#!/usr/bin/env bash
set -euo pipefail

BIN="${INSYLUS_SERVER_BIN:-${INSYLUS_BIN_DIR:-/opt/insylus/bin}/insylus-server}"
DB="${INSYLUS_DB_PATH:-${INSYLUS_DATA_DIR:-/var/lib/insylus}/insylus.db}"
SSH_USER="${INSYLUS_MANAGED_USER:-insylusmgr}"
IDENTITY_FILE="${INSYLUS_SSH_IDENTITY_FILE:-/home/$SSH_USER/.ssh/id_ed25519}"
SYSTEMD_DIR="${INSYLUS_SYSTEMD_DIR:-/etc/systemd/system}"
SERVER_SERVICE_NAME="${INSYLUS_SERVICE_NAME:-insylus.service}"
SYNC_SERVICE_NAME="${INSYLUS_SSH_SYNC_SERVICE_NAME:-insylus-ssh-sync.service}"
SYNC_TIMER_NAME="${INSYLUS_SSH_SYNC_TIMER_NAME:-insylus-ssh-sync.timer}"
SERVICE_PATH="$SYSTEMD_DIR/$SYNC_SERVICE_NAME"
TIMER_PATH="$SYSTEMD_DIR/$SYNC_TIMER_NAME"

cat >"$SERVICE_PATH" <<EOF
[Unit]
Description=Sync managed SSH aliases from Insylus
After=network-online.target $SERVER_SERVICE_NAME
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=$BIN sync-managed-ssh -db $DB -ssh-user $SSH_USER -identity-file $IDENTITY_FILE
EOF

cat >"$TIMER_PATH" <<EOF
[Unit]
Description=Periodic managed SSH alias sync for Insylus

[Timer]
OnBootSec=30s
OnUnitActiveSec=1min
Unit=$SYNC_SERVICE_NAME

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable --now "$SYNC_TIMER_NAME"
systemctl start "$SYNC_SERVICE_NAME"
systemctl status --no-pager "$SYNC_SERVICE_NAME" || true
systemctl status --no-pager "$SYNC_TIMER_NAME"
