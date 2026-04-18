# Insylus Development Notes For Agents

This file is for coding agents working on Insylus. It captures project-specific context that is easy to miss from code alone.

## Workspace And Runtime

- Treat `/opt/insylus` as the project root.
- Do not introduce or revive old AccessMonitor naming/paths. If `accessmonitor` appears, treat it as legacy migration/removal compatibility only.
- This checkout may not be a git repository. Do not assume git metadata is available.
- This checkout is both the development workspace and the local app install root. Be careful with ownership and permissions.
- The development tree should remain owned by the human development user/group, currently `aia:appgroup`.
- Expected directory modes:
  - `/opt/insylus`: `2775`
  - `/opt/insylus/bin`: `2775`
  - `/opt/insylus/dist`: `2775`
- Local runtime service is `insylus.service`.
- Installed controller binaries live in `/opt/insylus/bin`.
- Built release artifacts live in `/opt/insylus/dist`.
- Production SQLite database is `/var/lib/insylus/insylus.db`.
- Primary local checks:
  - `systemctl status --no-pager insylus.service`
  - `insylus --help`
  - `insylusctl devices`

## Permissions And Sudo Rules

This project has been broken before by building or installing as root. Do not repeat that.

- Do not run `go build` with `sudo`.
- Do not write build artifacts as `root` into `/opt/insylus/dist`.
- Do not make `/opt/insylus`, `/opt/insylus/bin`, or `/opt/insylus/dist` owned by `root:root`.
- Do not run blanket permission fixes that remove execute bits from binaries or scripts.
- Use `sudo` only for actions that genuinely require system privileges:
  - running `/opt/insylus/scripts/install-insylus-service.sh`
  - restarting or inspecting system services
  - editing files under `/etc/systemd/system`, `/usr/local/bin`, or `/var/lib/insylus`
- The installer is intentionally written to copy binaries into `/opt/insylus/bin` as the dev owner/group, not as root. Preserve that behavior.
- `/var/lib/insylus` and `/var/lib/insylus/insylus.db` are runtime state for the `insylus` service account. Do not chown them to the development user unless explicitly asked.

If permissions are damaged, prefer this repair pattern:

```bash
namei -l /opt/insylus
sudo chown -R aia:appgroup /opt/insylus
sudo find /opt/insylus -type d -exec chmod 2775 {} \;
sudo find /opt/insylus -type f -exec chmod 664 {} \;
sudo find /opt/insylus/bin /opt/insylus/dist /opt/insylus/scripts -type f -exec chmod 775 {} \;
```

After any permission repair, verify the command shims still work:

```bash
insylus --help
insylusctl devices
find /opt/insylus \( -user root -o -group root \) -print
```

The final `find` command should print nothing for this development tree.

## Product Shape

- Product name is `Insylus`.
- Agent service on devices is `insylus-agent.service`.
- One agent binary supports multiple device modes. New devices default to `inventory-only`; existing migrated devices remain `access-managed`.
- CLI commands are `insylus` and `insylusctl`.
- OpenClaw skill lives under `openclaw-skills/insylus`.
- `AGENT_GUIDE.md` is the operator/AI usage guide. Keep it aligned when CLI/API behavior changes.

## Build, Redeploy, And Verify

When implementing changes that affect the running app, rebuild and redeploy locally unless the user explicitly asks not to.

Recommended verification flow. Run the `go` commands as the normal development user from `/opt/insylus`; do not prefix them with `sudo`.

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

If `insylus` or `insylusctl` fail with `Permission denied`, check execute bits on `/opt/insylus/bin/insylusctl` and the symlink target path before changing code:

```bash
ls -l /usr/local/bin/insylus /usr/local/bin/insylusctl /opt/insylus/bin/insylusctl
namei -l /usr/local/bin/insylusctl
```

If `insylusctl devices` fails immediately after a service restart with `connection refused`, wait a second and retry once before assuming the service is broken.

If agent behavior changes and enrolled devices should receive it through auto-update, bump `internal/version/version.go`. Same-version binary changes intentionally do not trigger agent auto-update.

## Agent Auto-Update

- Auto-update is server controlled.
- Existing deployed agents needed one manual reinstall to gain auto-update support.
- The server serves architecture-specific binaries from `dist`.
- Agent updates validate SHA256 and replace the binary atomically before exiting for `systemd` restart.
- If you change agent collection, policy enforcement, install behavior, or update behavior, rebuild all agent target binaries and verify fleet versions with `insylusctl devices`.
- Agent policy enforcement must respect `device_mode`. `inventory-only` must not create, lock, unlock, or delete users, SSH keys, sudoers, or groups.

## Inventory And Topology Intent

Insylus v2 is homelab inventory plus access control. Preserve these concepts:

- `device_mode` is capability scope: `inventory-only` means observe/report only; `access-managed` means managed-account policy may be enforced.
- `device_type` is topology form: `bare-metal`, `vm`, `lxc`, `container`, or `unknown`.
- `purpose` is role/capability: discovered values may be `docker-host` or `proxmox-node`, while manual overrides may be any short human label such as `OpenClaw host`, `Coding server`, `Discord bot`, or `Living picture frame`.
- A Proxmox node can be bare-metal and have purpose `proxmox-node`.
- A Docker host can be a VM and have purpose `docker-host`.
- `platform_class` is hardware/platform, such as `raspberry-pi` or `generic-linux`.
- `parent=none` means the device normally has no parent.
- `parent=unknown` means the device probably has a parent, but Insylus has not matched it.
- Manual overrides for type, purpose, and parent must not be overwritten by discovery until cleared.
- Device notes are always manual and must never be changed by discovery.

## Output Shape

Keep structured output agent-friendly:

- `insylusctl devices --json` is intentionally compact and should only expose `name`, `hostname`, `ips`, and `purpose`.
- Prefer `insylusctl devices --find VALUE --json` for single-device lookup by name, hostname, IP, or ID.
- `--find` is preferred over requiring a user or agent to discover an opaque device ID first.
- `--json --info` is for normal single-device inspection.
- `--json --full` is for deep inspection and can include health, workloads, children, and discovery warnings.
- Preserve deterministic JSON grouping/order for readability.
- Keep compact output small. Do not add fields to compact casually.

When changing API/CLI output, update:

- `Insylusv2OutputShapePlan.md`
- `AGENT_GUIDE.md`
- `openclaw-skills/insylus`

## Workload Discovery Signal

Workload discovery should show operator-relevant things, not every system daemon.

- Keep visible: user/application services, `docker`, `insylus-agent`, `pulse-agent`, `qemu-guest-agent`, discovered Docker containers, Proxmox VMs, and Proxmox LXCs.
- Filter noisy platform plumbing such as routine `systemd-*`, Proxmox backend daemons, Wi-Fi/display helper services, storage monitors, and base OS services.
- Expect real hosts to reveal more noise over time. It is okay to tune the ignore list in small verified passes.

## Pre-Public Release Warning

Before pushing Insylus publicly, review `PREPUBLIC_RELEASE_NOTES.md`.

Important known cleanup:

- `aia_enabled` remains as a v1 compatibility field and should be renamed to neutral managed-account terminology before public release.
- The managed remote account is still hardcoded as `aia` in agent/server behavior. This must be made configurable or neutralized before public release.
- Avoid adding new `aia`-specific public surface area.

## Safety

- Preserve access-control behavior unless the task is explicitly about changing it.
- Do not delete or reset the production database.
- Do not remove legacy migration code unless the user explicitly decides the old path is no longer needed.
- Prefer additive migrations and compatibility-preserving API changes unless a plan explicitly says the shape is intentionally changing.
