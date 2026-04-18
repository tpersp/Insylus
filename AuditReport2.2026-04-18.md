# Insylus Audit Report

Ongoing audit notes for Insylus homelab inventory application.

---

## Known Issues

### High Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | 6 panic() calls in init/config paths (cmd/insylus-agent/main.go 41,47; cmd/insylusctl/main.go 17; plugins/agent/handlers.go 228; internal/server/app.go 133; internal/server/store.go 1795) | Intentional - fatal init errors crash process |
| 2026-04-18 | Hardcoded paths tied to app installation (/opt/insylus, /etc/sudoers.d/insylus-*, /home/insylus/, /var/lib/insylus/) | Intentional - system paths are fixed at install |

### Medium Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | ~85 ignored error patterns - many reviewed as intentional | Best-effort response writes are intentional |
| 2026-04-18 | InsecureSkipVerify: true in plugins/proxmox/client.go and plugins/jellyfin/client.go | Documented as user opt-in; acceptable for homelab |
| 2026-04-18 | Rows not properly closed with defer in store.go files (multiple locations) | Minor - connection already processed |
| 2026-04-18 | Time parsing ignored in store.go | Intentional - results in zero time for bad data |

### Low Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | cmd/insylus-agent has no test files | Low priority |
| 2026-04-18 | cmd/insylusctl has no test files | Low priority |
| 2026-04-18 | Hardcoded port 8080 in cmd/insylus-server/main.go | Acceptable default |
| 2026-04-18 | Hardcoded timeouts (20s, 30s, 2m, etc.) | Acceptable for homelab use |

---

## Questions & Answers

### Open Questions

| Date | Question | Notes |
|------|----------|-------|
| 2026-04-18 | None | No open audit questions remain. |

### Resolved (from previous audit 2026-04-18)

| Date | Question | Answer |
|------|----------|--------|
| 2026-04-18 | Was v1 account-specific terminology removed? | Yes - public output uses `managed_account_enabled` |
| 2026-04-18 | Should ManagedUser be configurable? | Yes - global controller setting persisted in app_settings |
| 2026-04-18 | Should managed groups be configurable? | Yes - global controller setting persisted in app_settings |
| 2026-04-18 | Should ignored errors in plugins be fixed? | Reviewed - state-changing errors fixed; response writes/flushes remain best-effort |
| 2026-04-18 | Should access/store.go handle all errors? | Yes - RowsAffected/LastInsertId/timestamp errors now handled |
| 2026-04-18 | Are plugins without tests a problem? | Some tests added; low priority remaining |
| 2026-04-18 | Are all ~85 ignored errors intentional? | Mostly yes; system_linux.go cleanup omissions were tightened so sudo/audit cleanup failures now fail policy enforcement instead of being ignored. |
| 2026-04-18 | Does Settings-based ManagedUser config work end-to-end? | Yes - covered by `TestSettingsManagedAccountFormUpdatesAgentPolicy`, which posts `/settings/managed-account` and verifies `/api/policy` uses the persisted user. |
| 2026-04-18 | Does Settings-based ManagedGroups config work end-to-end? | Yes - covered by `TestSettingsManagedAccountFormUpdatesAgentPolicy`, including trimming and deduplicating groups before agent policy output. |

---

## Enhancement Ideas

| Date | Idea | Priority |
|------|------|----------|
| 2026-04-18 | Expand test coverage to cmd/insylus-agent and cmd/insylusctl | Low |
| 2026-04-18 | Add warning UI when InsecureSkipVerify enabled for Proxmox/Jellyfin | Low |

---

## Code Audit Summary

### Intentional Hardcoded Values

The following are intentionally hardcoded - these are the system user and paths installed with the app:

**System user (managed account):**
- "insylus" is the dedicated service user installed with the app
- Runs the insylus.service
- Used as the managed account on access-managed devices
- Default is configurable via Settings, persisted globally

**System paths:**
- `/opt/insylus` - app install directory
- `/etc/sudoers.d/insylus-*` - policy files
- `/home/insylus/` or `/var/lib/insylus/` - service user home
- `/var/lib/insylus/insylus.db` - SQLite database

**Groups (for audit mode):**
- "adm" and "systemd-journal" are standard Linux groups for log access

### Panic Usage (6 locations)

All panic() calls are intentional for fatal initialization errors - if config fails or crypto is unavailable, the process cannot function:

| File | Line | Context |
|------|------|---------|
| cmd/insylus-agent/main.go | 41 | LoadConfig failure |
| cmd/insylus-agent/main.go | 47 | runner.Run failure |
| cmd/insylusctl/main.go | 17 | plugin registration |
| plugins/agent/handlers.go | 228 | randomToken crypto failure |
| internal/server/app.go | 133 | embed static files |
| internal/server/store.go | 1795 | randomToken crypto failure |

### Security Review

- **SQL Injection**: No risk - parameterized queries used throughout
- **Command Injection**: Properly mitigated in docker client with quote escaping
- **Hardcoded Secrets**: None found - all tokens/keys generated at runtime
- **Crypto**: Uses crypto/rand properly - no weak crypto
- **TLS**: InsecureSkipVerify documented as opt-in for self-signed certs

### Test Coverage

**Has tests:**
- internal/agent ✓
- internal/server ✓
- plugins/access ✓
- plugins/wake ✓

**No tests:**
- cmd/insylus-agent
- cmd/insylusctl
- internal/api
- internal/ctl
- internal/finder
- internal/format
- internal/pluginhost
- internal/shared
- internal/version
- plugins/agent
- plugins/devices
- plugins/docker
- plugins/help
- plugins/jellyfin
- plugins/proxmox
- plugins/registry
- plugins/services
- plugins/topology

---

## TODO

- [x] Confirm Settings-based ManagedUser config works end-to-end
- [x] Verify Settings-based ManagedGroups config works end-to-end
- [x] Review system_linux.go cleanup error handling (lines 90, 93, 114-115, 164)

---

## Architecture Notes

- Current version: 0.1.14 (internal/version/version.go)
- Uses SQLite backend (/var/lib/insylus/insylus.db)
- 10 plugins: access, agent, devices, docker, help, jellyfin, proxmox, services, topology, wake
- Two device modes: inventory-only (safe default), access-managed
- Agent auto-update supported via server-served binaries
- Managed account persisted in app_settings (global controller setting)
- System user "insylus" is installed with the app and runs the service

---

## Previous Audit Summary

The previous audit (dated 2026-04-18) addressed:
- Hardcoded managed user and groups → Fixed via Settings persistence
- ~25 ignored errors → Reviewed, fixed state-changing, kept best-effort
- access/store.go error handling → Fixed
- No tests in plugins/ → Added tests for access

_Last updated: 2026-04-18_
