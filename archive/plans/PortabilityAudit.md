# Portability Audit

Date: 2026-04-18

Archived 2026-04-18. This audit is complete for the current pre-public
portability pass.

This audit tracks hardcoded deployment assumptions and the current portability status for Insylus.

## Status

Mostly implemented for the practical controller and agent deployment paths. The remaining fixed names are intentional product/service names, compatibility paths, or defaults that are now documented and configurable where useful.

## Implemented

- Controller installer defaults are configurable through environment variables:
  - `INSYLUS_INSTALL_ROOT`
  - `INSYLUS_BIN_DIR`
  - `INSYLUS_DATA_DIR`
  - `INSYLUS_LISTEN_ADDR`
  - `INSYLUS_SYSTEMD_DIR`
  - `INSYLUS_COMMAND_DIR`
  - `INSYLUS_APP_USER`
  - `INSYLUS_APP_GROUP`
  - `INSYLUS_SERVICE_NAME`
  - `INSYLUS_SERVER_BIN_SRC`
  - `INSYLUS_AGENT_BIN_SRC`
  - `INSYLUS_CTL_BIN_SRC`
  - `INSYLUS_CTL_LINK_DST`
  - `INSYLUS_LINK_DST`
- Managed remote access account policy is explicit and config-driven:
  - `cmd/insylus-server` exposes `-managed-user` and `-managed-groups`.
  - `scripts/install-insylus-service.sh` accepts `INSYLUS_MANAGED_USER` and `INSYLUS_MANAGED_GROUPS`.
  - The Settings page persists managed user/groups in `app_settings`.
  - Agent policy responses include `managed_user`, `managed_groups`, `sudoers_path`, `audit_readme_path`, `authorized_keys_path`, and `account_state`.
  - `internal/agent/system_linux.go` uses policy fields instead of hardcoded remote account, sudoers, audit-group, and authorized-keys paths.
- Controller service account and managed remote account are documented as separate concepts:
  - `INSYLUS_APP_USER` controls the local service account that runs `insylus.service`.
  - `INSYLUS_MANAGED_USER` controls the optional remote account managed on enrolled devices.
- Managed SSH sync installer follows controller path settings:
  - `INSYLUS_BIN_DIR`
  - `INSYLUS_DATA_DIR`
  - `INSYLUS_DB_PATH`
  - `INSYLUS_SYSTEMD_DIR`
  - `INSYLUS_SERVICE_NAME`
  - `INSYLUS_SSH_SYNC_SERVICE_NAME`
  - `INSYLUS_SSH_SYNC_TIMER_NAME`
  - `INSYLUS_SSH_IDENTITY_FILE`
- Remote agent install paths are configurable on target hosts:
  - `INSYLUS_AGENT_BIN_PATH`
  - `INSYLUS_AGENT_CONFIG_PATH`
  - `INSYLUS_AGENT_SERVICE_NAME`
  - `INSYLUS_AGENT_UNIT_PATH`
- Agent auto-update uses the configured installed agent path, inherited through the generated systemd unit environment.
- `scripts/deploy.sh` no longer assumes `/opt/insylus` and uses systemd restart when `insylus.service` exists.
- CLI plugins honor `INSYLUS_SERVER_URL` before falling back to `http://127.0.0.1:8080`.
- Plugin migrations completed:
  - `plugins/wake` owns Wake-on-LAN web/API handlers and implementation.
  - `plugins/services` owns service web/API handlers, view shaping, and query wrapper.
  - `plugins/access` owns access settings, key management, policy, device mode, and agent auto-update web handlers.
  - `plugins/agent` owns bootstrap/checkin/policy/report/download handlers.
  - `plugins/topology` owns graph and manual topology handlers.
  - `plugins/devices` owns the shaped `/api/devices` inventory API through the generic inventory service.
- Tests cover non-default managed account policy and non-default agent install paths.

## Intentional Defaults

These defaults remain, but are either product names or can now be overridden:

- Controller service: `insylus.service`
- Controller service account: `insylus`
- Controller install root: `/opt/insylus`
- Controller data directory: `/var/lib/insylus`
- Controller command links: `/usr/local/bin/insylus` and `/usr/local/bin/insylusctl`
- Controller listen address: `:8080`
- Remote agent service: `insylus-agent.service`
- Remote agent binary: `/usr/local/bin/insylus-agent`
- Remote agent config: `/etc/insylus-agent/config.json`
- Remote managed account default: `insylus`
- Remote audit group defaults: `adm,systemd-journal`

## Remaining Hardcoded Assumptions

- Compatibility-only legacy names remain in uninstall/migration paths:
  - `accessmonitor-agent.service`
  - `/etc/accessmonitor-agent/config.json`
  - `/usr/local/bin/accessmonitor-agent`
- `internal/agent/system_linux.go` keeps `insylus` path prefixes and marker text as compatibility fallbacks when old servers do not send explicit policy paths.
- `/downloads/insylus-agent` and binary filenames such as `insylus-agent-linux-amd64` are product API/artifact names.
- Local examples in docs and archived plans still mention names such as `MiscServer`, `beta-pve`, `atlas`, `/opt/insylus`, and `127.0.0.1:8080`. Current user-facing docs label the important ones as defaults/examples; archived plans are historical.
- Rich device-detail web composition still depends on generic backbone data from the server store. This is acceptable until there is a narrower device-detail service or the device page is split into smaller plugin-owned panels.

## Managed-User Compatibility Plan

1. Keep the neutral `managed_account_enabled` field as the public concept.
2. Keep any legacy storage/API compatibility fields until the pre-public cleanup explicitly removes them.
3. Keep remote managed account defaults stable for current installs.
4. DB-backed Settings UI exists for managed user/groups; keep process/systemd flags as install-time defaults and recovery overrides.
5. After the compatibility window, remove legacy account-specific terminology from DB/UI/API surfaces if any remains.

## Plugin Migration Status

Reference self-contained plugins:

- `plugins/proxmox`
- `plugins/jellyfin`
- `plugins/wake`
- `plugins/services`
- `plugins/access`
- `plugins/agent`
- `plugins/topology`

Remaining bridge area:

- Rich device-detail web composition still spans inventory, access, agent, topology, wake, and services. Keep future work incremental: move individual panels behind generic services instead of copying server store internals into `plugins/devices`.

Guidelines for future migration:

- Keep existing route paths compatible.
- Run `go test ./...`.
- Run `go build ./cmd/insylusctl`.
- Run `go build ./cmd/insylus-server`.
- Do not add feature-specific host methods under `internal/pluginhost`.
- Do not add feature-specific implementation files under `internal/server`.

## Verification

- `bash -n scripts/install-insylus-service.sh scripts/install-insylus-ssh-sync.sh scripts/deploy.sh`
- `go test ./...`
- Local service rebuild/reinstall/restart flow after code changes that affect the running app.
