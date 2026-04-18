# TODO

Status 2026-04-18: this is the active Insylus task tracker. Completed planning docs stay in `archive/plans/`; this file keeps the current state and the next useful work visible.

## Recently Completed

- [x] Portability audit refreshed and archived in `archive/plans/PortabilityAudit.md`.
- [x] Controller install paths, service settings, managed-user/groups, managed SSH sync paths, and remote agent install paths are configurable through `INSYLUS_*` environment variables.
- [x] Managed user/groups can be changed from the Settings page and are persisted in `app_settings`.
- [x] Agent policy output uses explicit managed-account fields: `managed_user`, `managed_groups`, `sudoers_path`, `audit_readme_path`, `authorized_keys_path`, and `account_state`.
- [x] CLI plugins honor `INSYLUS_SERVER_URL` before falling back to `http://127.0.0.1:8080`.
- [x] Feature plugins migrated away from server-owned handler bundles where practical: `wake`, `services`, `access`, `agent`, `topology`, and the shaped devices inventory API.
- [x] Docker plugin implemented and archived in `archive/plans/DockerPluginPlan.md`.
- [x] Topology V1 and V2 implemented and archived in `archive/plans/InsylusTopologyMapPlan.md`: manual graph nodes/links, node editing, link label editing, saved layout positions, and reset layout.
- [x] `AGENT_GUIDE.md` and OpenClaw Insylus skill updated for Docker and topology layout behavior.

## Active Follow-Up

- [x] Docker plugin is currently piggybacking on the device plugin — it requires users to *select* an existing device host rather than allowing independent Docker host configuration. Rework so it can function standalone: add ability to input IP/hostname and Docker credentials directly in the plugin, similar to how the Proxmox plugin works (with an *option* to link to a device, but connection info controlled purely in the plugin). If the devices-plugin is removed, the Docker plugin becomes non-functional — fix this.
- [x] Access plugin web UI has multiple clarity/usability issues (internal/server/templates/access_settings.html):
  1. Fixed "How It Works → Managed User Behavior" section — span/strong pairs now have proper CSS spacing with labels ending in colons.
  2. Managed user field is now blank with placeholder "remote-account" instead of pre-filled value.
  3. Consolidated access mode and audit groups into single "Access level" dropdown: audit (read logs), docker (manage containers), sudo (prompted), sudo (passwordless). Groups are derived automatically from access level (audit→adm, docker→docker). Removed separate free-text audit groups field.
- [ ] Keep `docs/HowToBuildPlugin.md`, `docs/AGENT_GUIDE.md`, and `openclaw-skills/insylus` aligned whenever CLI/API/plugin behavior changes.
- [ ] Before public release, review `PREPUBLIC_RELEASE_NOTES.md` and remove or neutralize remaining compatibility terminology such as `aia_enabled` if it still exists.
- [ ] Decide whether rich device-detail web composition should stay as a cross-plugin page or be split into smaller plugin-owned panels.
- [ ] Decide whether topology gateway/subnet discovery should become an active plan. If yes, create a new plan from the future-ideas section in `archive/plans/InsylusTopologyMapPlan.md`.
- [ ] Decide whether vendor topology integrations such as SNMP, UniFi, Omada, MikroTik, or LLDP should become a separate active plan.
- [ ] Review `plans/PluginIdeas.md` and promote any idea worth building into a focused plan.

## Maintenance Checks

- [ ] After plugin migrations or route changes, run `go test ./...`.
- [ ] After controller/web changes, rebuild and redeploy locally:

```bash
go test ./...
go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl
go build -o /opt/insylus/dist/insylus-agent ./cmd/insylus-agent
env GOOS=linux GOARCH=amd64 go build -o /opt/insylus/dist/insylus-agent-linux-amd64 ./cmd/insylus-agent
env GOOS=linux GOARCH=arm64 go build -o /opt/insylus/dist/insylus-agent-linux-arm64 ./cmd/insylus-agent
env GOOS=linux GOARCH=arm GOARM=7 go build -o /opt/insylus/dist/insylus-agent-linux-armv7 ./cmd/insylus-agent
sudo bash /opt/insylus/scripts/install-insylus-service.sh
sudo systemctl restart insylus.service
insylusctl devices
```

## Guardrails

- Do not introduce new `accessmonitor` naming except for legacy migration/removal compatibility.
- Do not add feature-specific host methods to `internal/pluginhost`; prefer generic services such as `host.DB()`, `host.Targets()`, `host.Data().Inventory()`, `host.Secrets()`, and capability registration.
- Do not add new feature-owned implementation files under `internal/server` when a plugin-owned file can own the behavior.
- Keep compact JSON output small; do not casually add fields to `insylusctl devices --json`.
- Preserve `inventory-only` behavior: agents in that mode must not create, lock, unlock, or delete users, SSH keys, sudoers, or groups.
