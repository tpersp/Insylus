# Insylus v2 Plan: Safe `insylus-agent` Auto-Update

---

**IMPLEMENTED** — Core auto-update functionality is present in the codebase.

## Summary

Add opt-in automatic updates for `insylus-agent` so enrolled devices can update themselves from the controller without SSH/manual reinstall. The feature will use the server-served arch-specific agent binaries, verify downloads by checksum, replace the agent binary atomically, and restart by exiting cleanly so `systemd` restarts the service via the existing `Restart=always` unit.

Chosen defaults:
- global default plus per-device override
- disabled by default for existing and new devices
- update on next agent check-in when enabled
- update only when server agent version is newer
- verify the downloaded binary checksum before replacing anything
- on failure, keep the current agent running and report the error

## Key Changes

### Versioning and Server Manifest

- Move the agent version constant into a shared/internal version package so both server and agent use the same release version.
- Add an authenticated agent update manifest to the policy flow, preferably by extending `GET /api/policy?goos=...&goarch=...`.
- Manifest fields:
  - `enabled`
  - `server_agent_version`
  - `download_url`
  - `sha256`
  - `goos`
  - `goarch`
- Server computes `sha256` from the same resolved binary path used by `/downloads/insylus-agent`.
- Agent compares versions with a simple numeric dotted version comparator, such as `0.1.0 < 0.1.1`.
- Same-version checksum changes do not trigger auto-update; release process must bump the agent version when shipping an agent update.

### Settings and Status

- Add global setting storage for `agent_auto_update_default`, default `false`.
- Add per-device override state:
  - `inherit`
  - `enabled`
  - `disabled`
- Effective value:
  - `inherit` uses the global default
  - explicit per-device value wins over global default
- Store/update agent update status per device:
  - `agent_auto_update_enabled`
  - `agent_auto_update_override`
  - `agent_update_available`
  - `server_agent_version`
  - `agent_update_status`: `idle | available | updating | updated | failed | unsupported`
  - `agent_update_error`
  - `agent_update_last_checked_at`
  - `agent_update_last_attempted_at`
- Extend health/check-in data with agent runtime platform:
  - `agent_goos`
  - `agent_goarch`

### Agent Behavior

- Agent sends `goos/goarch` when fetching policy.
- During each sync:
  - check in health
  - fetch policy/update manifest
  - if auto-update is enabled and server version is newer, attempt update before normal policy enforcement
  - if update is not needed, continue normally
- Update algorithm:
  - download the matching arch binary to a temp file in `/usr/local/bin`
  - verify SHA256 against the server manifest
  - chmod executable
  - optionally run the temp binary with `version` to confirm it starts and reports the expected version
  - atomically rename it over `/usr/local/bin/insylus-agent`
  - report/update status when possible
  - exit cleanly so `systemd` restarts the service using the new binary
- On download, checksum, validation, or rename failure:
  - do not replace the current binary
  - continue running the existing agent
  - report failure status and error text to the server
- Add `insylus-agent version` command for validation and diagnostics.

### UI, API, and CLI

- Web UI:
  - add global auto-update default control on the home/settings area
  - add per-device control on the device page: `inherit`, `enabled`, `disabled`
  - show effective auto-update state, current agent version, server agent version, update availability, and last update status/error
- API/inventory:
  - include auto-update fields in `info` and `full` views
  - keep compact output unchanged
- CLI:
  - include auto-update fields in `--json --info` and `--json --full`
  - add a compact table indicator such as an `AGENT` column showing current version and update state, without adding mutation commands
- No interactive installer prompt in v1 of this feature; piped one-liners should stay non-interactive. Devices inherit the server global default unless overridden in the UI/API.

## Test Plan

- Store/migration tests:
  - new settings and per-device update columns/tables are created and backfilled
  - existing devices default to inherited disabled auto-update
  - effective value resolves global default plus per-device override correctly
- Server/API tests:
  - policy response includes update manifest when `goos/goarch` are provided
  - manifest resolves the correct arch-specific binary and checksum
  - missing binary returns disabled/unsupported update metadata rather than breaking normal policy fetch
  - inventory info/full include auto-update fields and compact excludes them
- Agent tests:
  - version comparison handles equal, older, newer, empty, and malformed versions
  - checksum mismatch does not replace the current binary
  - successful validation uses atomic replacement
  - failed update records error and continues normal sync
  - successful update exits/restarts path without requiring `systemctl restart`
- Acceptance scenarios:
  - device with auto-update disabled never attempts update
  - device inheriting global enabled updates on next check-in when server version is newer
  - per-device disabled override blocks a globally enabled default
  - bad checksum leaves old agent running and surfaces an error in UI/API/CLI
  - after successful update, device reports the new `agent_version` on the next check-in

## Assumptions and Defaults

- Auto-update is disabled by default.
- Existing deployed agents must be manually reinstalled once to receive the first auto-update-capable agent.
- Auto-update is controlled by the server, not by local interactive install prompts.
- The existing `Restart=always` systemd unit is required for self-restart after update.
- The server and agent version must be bumped for each agent release; same-version binary changes are intentionally not auto-applied.
- Rollback is not implemented in this first version; failure safety comes from validating before replacement and keeping the old binary when validation fails.
