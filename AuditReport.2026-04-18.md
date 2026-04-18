# Insylus Audit Report

Ongoing audit notes for Insylus homelab inventory application.

---

## Known Issues

### High Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | Hardcoded default managed user "insylus" - should be configurable | Fixed - global server config plus persisted Settings UI |
| 2026-04-18 | Managed groups hardcoded to ["adm", "systemd-journal"] | Fixed - global server config plus persisted Settings UI |

### Medium Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | ~25+ instances of ignored errors with `_ =` pattern across plugins | Reviewed - fixed state-changing omissions; remaining response writes, table flushes, and best-effort cleanup are intentional |
| 2026-04-18 | 7+ instances of `if err == nil` without else error handling | Reviewed - fixed missing non-`ErrNoRows` handling in agent/proxmox/jellyfin paths; remaining cases are expected control flow |
| 2026-04-18 | access/store.go has unhandled db.ExecContext errors (lines 86, 92, 171) | Fixed - `RowsAffected`, `LastInsertId`, and timestamp parse errors are checked |

### Low Priority

| Date | Issue | Status |
|-----|-------|--------|
| 2026-04-18 | Plugins directory has no test files | Fixed - added `plugins/access/store_test.go`; existing `plugins/wake/wol_test.go` retained |
| 2026-04-18 | cmd/ directory has no test files | Fixed - added `cmd/insylus-server/main_test.go` |

---

## Questions & Answers

### Open Questions

None.

### Resolved

| Date | Question | Answer |
|------|----------|--------|
| 2026-04-18 | Was v1 account-specific terminology removed? | Public output uses `managed_account_enabled`; any legacy storage/API compatibility should remain only until the explicit pre-public cleanup. |
| 2026-04-18 | Should ManagedUser be configurable per-device, per-plugin, or just global server config? | Global controller setting for now, editable from Settings and persisted in `app_settings`. Per-device UI can be added later without agent changes. |
| 2026-04-18 | Should managed groups be configurable per-device or per-plugin? | Global controller setting for now, editable from Settings and persisted in `app_settings`. Per-device groups remain an enhancement. |
| 2026-04-18 | Is it intentional to ignore errors in plugins/agent/handlers.go? | Partly. Script/JSON response writes remain best-effort. Target update errors are now handled and returned as server errors. |
| 2026-04-18 | Is it intentional to ignore errors in plugins/proxmox/store.go? | Partly. Missing inventory enrichment is allowed only for `sql.ErrNoRows`; other inventory and timestamp errors now return errors. |
| 2026-04-18 | Is it intentional to ignore errors in plugins/jellyfin/*? | Formatter flush and JSON response writes are intentionally best-effort. Store inventory and timestamp errors now return errors. |
| 2026-04-18 | Is wake plugin intentionally ignoring conn.Write error? | No. `sendWakePacket` already returns the `conn.Write` error; the audit line appears stale. JSON response encode errors remain best-effort. |
| 2026-04-18 | Should access/store.go handle db.ExecContext errors? | Yes. The store already handled `ExecContext` errors, and now also handles `RowsAffected`, `LastInsertId`, and timestamp parse errors. |

---

## Enhancement Ideas

| Date | Idea | Priority |
|------|------|----------|
| 2026-04-18 | Add per-device ManagedUser configuration UI | Medium |
| 2026-04-18 | Add per-device managed groups configuration UI | Medium |
| 2026-04-18 | Expand test coverage across individual plugins | Low |
| 2026-04-18 | Add tests for `cmd/insylus-agent` and `cmd/insylusctl` argument handling | Low |

---

## Code Audit Summary

### Error Handling Issues

**Ignored errors with `_ =`**:

- Fixed: flag parsing in `cmd/insylus-agent` and `cmd/insylus-server`; target updates in `plugins/agent`; topology link delete; Proxmox/Jellyfin inventory enrichment and timestamp parsing; Access insert-id/timestamp handling.
- Intentional: HTTP response writes/encodes after status is already selected, table writer flushes in CLI formatters, `devices/plugin.go` placeholder assignment, and best-effort cleanup/reporting where a later explicit error is returned.

**Missing else error handling**:

- Fixed where non-`ErrNoRows` errors were previously hidden.
- Left as intentional where `err == nil` is the success branch and the non-success branch is already handled by helper predicates or expected fallback behavior.

### Configurability Issues

- **ManagedUser**: Persisted Settings value is wired into policy responses and the agent plugin compatibility route; server config remains the install-time fallback.
- **Managed groups**: Persisted Settings value is wired into policy responses and agent enforcement; server config remains the install-time fallback.

### Test Coverage

Added focused tests for server managed account policy, agent managed policy parsing, command managed-group parsing, and access key fingerprinting.

Remaining low-priority gaps: individual command tests for `cmd/insylus-agent`/`cmd/insylusctl` and deeper plugin tests for agent/devices/docker/jellyfin/proxmox/services/topology.

---

## TODO

- [x] Review error handling - decide which ignored errors are intentional vs bugs
- [x] Decide on ManagedUser configurability approach
- [x] Decide on managed groups configurability
- [x] Add tests to plugins/ directory
- [x] Add tests to cmd/ directory

---

## Architecture Notes

- Current version: 0.1.13 (internal/version/version.go)
- Uses SQLite backend
- 11 plugins: access, agent, devices, docker, help, jellyfin, proxmox, registry, services, topology, wake
- Two device modes: inventory-only (safe default), access-managed
- Agent auto-update supported via server-served binaries

---

_Last updated: 2026-04-18_
