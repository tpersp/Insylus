# Insylus Agent Modes Plan: Inventory-Only By Default, Access-Managed When Enabled

---

**IMPLEMENTED** — `device_mode` field exists in types, store, and policy response. Agent skips access enforcement in `inventory-only` mode and runs `cleanupInventoryOnly` on mode switch.

## Summary

Rework `insylus-agent` into one binary with server-controlled capability modes. New enrollments default to `inventory-only`, meaning the agent reports health, topology, workloads, agent version, and update status, but does not touch users, SSH keys, sudoers, groups, or access policy. Operators can later switch a device to `access-managed`, where the existing managed-account behavior becomes available.

Chosen decisions:
- Use a separate `device_mode`, not a new `access_mode`.
- New devices default to `inventory-only`.
- Existing enrolled devices migrate as `access-managed` to preserve current behavior.
- Switching from `access-managed` to `inventory-only` removes only Insylus-owned access artifacts.
- Switching from `inventory-only` to `access-managed` starts as managed-disabled; audit/sudo must be explicitly selected afterward.

## Key Changes

### Data Model And Policy

Add a new enum-style field:

- `device_mode`: `inventory-only | access-managed`

Behavior:
- `inventory-only`: collect/report inventory, health, topology, workloads, auto-update status; skip access enforcement entirely.
- `access-managed`: run managed-account enforcement using existing access states.
- `access_mode` remains scoped to access management: `disabled | audit | sudo`.
- `aia_enabled` remains for compatibility for now, but UI should avoid making it the primary concept.
- Existing devices are backfilled to `access-managed`.
- Newly created devices default to `inventory-only` with `access_mode=disabled`.

Extend policy response with:
- `device_mode`
- existing access fields remain present for compatibility

Agent behavior:
- Always check in health.
- Always fetch policy/update manifest.
- Always auto-update when enabled.
- Always report topology/inventory.
- Only call access enforcement when `device_mode=access-managed`.

### Mode Transitions And Cleanup

When switching to `inventory-only`:
- Agent stops applying access policy.
- Agent runs a one-time cleanup of Insylus-owned artifacts:
  - remove `/etc/sudoers.d/insylus-aia`
  - remove `/etc/sudoers.d/insylus-aia-audit-readme`
  - remove Insylus-managed authorized key material only when safely identifiable
- Do not lock or delete the `aia` user.
- Do not remove groups like `adm` or `systemd-journal` unless ownership tracking proves Insylus added them.
- Report cleanup status/error in the normal device report.

Improve future ownership safety:
- Start writing managed SSH keys using an Insylus marker block/comment in `authorized_keys`, rather than blindly owning the whole file.
- For legacy devices where the file exactly matches the assigned Insylus key, cleanup may remove that exact key/file content.
- If the file contains unknown additional content and no marker, leave it untouched and report a cleanup warning.

When switching to `access-managed`:
- Do not immediately grant audit or sudo.
- Set access capability active, but keep `access_mode=disabled`.
- Operator must explicitly choose `audit` or `sudo`.
- When `audit` or `sudo` is selected, the agent may create/manage the `aia` user and required files as today.

### UI, API, CLI, And Docs

Web UI:
- Device detail page gets a clear `Device Mode` control:
  - `Inventory only`
  - `Access managed`
- Access policy controls are hidden/disabled unless mode is `access-managed`.
- In `inventory-only`, show explanatory copy: “Insylus will not modify users, SSH keys, sudoers, or groups on this device.”
- Add a warning when enabling `access-managed`, especially for controller-like hosts such as Atlas.

API/inventory:
- Include `device_mode` in `info` and `full` views.
- Keep compact JSON unchanged.
- Policy endpoint includes `device_mode`.

CLI:
- Device table adds a compact mode indicator, for example `MODE` with `inventory` or `managed`.
- `--json --info` and `--json --full` include `device_mode`.

Docs/OpenClaw:
- Update `AGENT_GUIDE.md`, `README.md`, `AGENTS.md`, and `openclaw-skills/insylus`.
- Document that “enrolled” means inventory participation, not necessarily access management.
- Document Atlas recommendation: enroll as `inventory-only`.

## Test Plan

Store/API tests:
- Migration adds `device_mode`.
- Existing devices become `access-managed`.
- New devices default to `inventory-only`.
- Policy response includes `device_mode`.
- Inventory info/full include `device_mode`; compact excludes it.

Agent tests:
- `inventory-only` skips access enforcement but still reports health/topology.
- `access-managed` preserves existing disabled/audit/sudo behavior.
- Switching to inventory-only removes Insylus sudoers files.
- Cleanup does not lock/delete the `aia` user.
- Cleanup removes marked authorized key blocks.
- Cleanup leaves unmarked mixed `authorized_keys` content alone and reports a warning.
- Auto-update still runs in both modes.

UI/acceptance:
- Freshly enrolled device appears as `Inventory only`.
- Atlas can be enrolled without changing local `aia` permissions.
- Existing devices keep current access behavior after migration.
- Operator can switch a device to access-managed, then choose audit or sudo.
- Operator can switch back to inventory-only and Insylus-owned access artifacts are removed without deleting the user.

## Assumptions And Defaults

- There will still be one `insylus-agent` binary.
- `inventory-only` is the default for new devices and installs.
- Existing devices remain `access-managed` to avoid breaking current access workflows.
- `access_mode=disabled` inside `access-managed` keeps its existing meaning: actively disable managed access.
- `inventory-only` means no access enforcement, not “disabled access enforcement.”
- Cleanup is conservative: remove only artifacts Insylus owns or can safely identify.
- Public-release cleanup of the hardcoded `aia` account remains a separate later refactor.
