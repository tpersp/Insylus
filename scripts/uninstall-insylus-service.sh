#!/usr/bin/env bash
set -euo pipefail

systemctl disable --now insylus-ssh-sync.timer 2>/dev/null || true
systemctl disable --now insylus-ssh-sync.service 2>/dev/null || true
systemctl disable --now insylus.service 2>/dev/null || true
rm -f /etc/systemd/system/insylus.service /etc/systemd/system/insylus-ssh-sync.service /etc/systemd/system/insylus-ssh-sync.timer
systemctl daemon-reload
