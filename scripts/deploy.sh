#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${INSYLUS_REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
INSTALL_ROOT="${INSYLUS_INSTALL_ROOT:-$REPO_ROOT}"
BIN_DIR="${INSYLUS_BIN_DIR:-$INSTALL_ROOT/bin}"
DATA_DIR="${INSYLUS_DATA_DIR:-/var/lib/insylus}"
LISTEN_ADDR="${INSYLUS_LISTEN_ADDR:-:8080}"
APP_USER="${INSYLUS_APP_USER:-insylus}"
SERVICE_NAME="${INSYLUS_SERVICE_NAME:-insylus.service}"
SERVER_LOG="${INSYLUS_SERVER_LOG:-/tmp/insylus-server.log}"
SKILL_FILE="${INSYLUS_SKILL_FILE:-$REPO_ROOT/openclaw-skills/insylus/SKILL.md}"
PLUGIN_MARKER="<!-- PLUGIN_SECTIONS -->"

collect_plugin_sections() {
    local out=""
    for plugin in "$REPO_ROOT"/plugins/*/; do
        local skill_md="${plugin}skill.md"
        if [[ -f "$skill_md" ]]; then
            out+="$(cat "$skill_md")"
            out+=$'\n'
        fi
    done
    echo "$out"
}

cd "$REPO_ROOT"

echo "Building binaries..."
mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/insylus-server" ./cmd/insylus-server
go build -o "$BIN_DIR/insylus-agent"  ./cmd/insylus-agent
go build -o "$BIN_DIR/insylusctl"     ./cmd/insylusctl

echo "Updating OpenClaw skill..."
sections=$(collect_plugin_sections)

# Replace the marker with all plugin sections
if grep -q "$PLUGIN_MARKER" "$SKILL_FILE"; then
    awk -v marker="$PLUGIN_MARKER" -v content="$sections" '
        found {buf = buf $0 ORS; next}
        index($0, marker) == 1 {
            found = 1
            printf "%s", content
            print ""
            next
        }
        {print}
    ' "$SKILL_FILE" > "${SKILL_FILE}.tmp" && mv "${SKILL_FILE}.tmp" "$SKILL_FILE"
else
    echo "" >> "$SKILL_FILE"
    echo "$PLUGIN_MARKER" >> "$SKILL_FILE"
    echo "$sections" >> "$SKILL_FILE"
fi

echo "  skill updated from $(find "$REPO_ROOT/plugins" -name skill.md | wc -l) plugin(s)"

echo "Restarting insylus service..."
if systemctl list-unit-files "$SERVICE_NAME" >/dev/null 2>&1; then
    sudo systemctl restart "$SERVICE_NAME"
    sudo systemctl status --no-pager "$SERVICE_NAME"
else
    sudo kill $(pgrep -f "insylus-server -listen") 2>/dev/null || true
    sleep 1
    sudo -u "$APP_USER" nohup "$BIN_DIR/insylus-server" -listen "$LISTEN_ADDR" \
        -db "$DATA_DIR/insylus.db" \
        -agent-binary "$BIN_DIR/insylus-agent" \
        > "$SERVER_LOG" 2>&1 &

    sleep 1
    if pgrep -f "insylus-server -listen" > /dev/null; then
        echo "insylus-server restarted successfully."
    else
        echo "ERROR: insylus-server failed to start. Check $SERVER_LOG"
        cat "$SERVER_LOG"
        exit 1
    fi
fi
