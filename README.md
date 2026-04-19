# Insylus

Insylus is a home-lab operations control plane for Linux device inventory, topology, health, and optional managed-account access.

For an AI-oriented command/reference guide, see [AGENT_GUIDE.md](/opt/insylus/AGENT_GUIDE.md).

## What is included

- Go server with SQLite-backed web UI and agent API
- Go agent with bootstrap, polling, host health/topology reporting, auto-update, and optional policy enforcement
- One-liner install flow that downloads the agent, registers the host, and installs a `systemd` service
- SSH key registry with fingerprinting on the controller host

## Run the server

```bash
go run ./cmd/insylus-server -listen :8080 -db ./insylus.db
```

If you want the generated install script to download the agent from the controller host, build the agent and pass its path:

```bash
go build -o dist/insylus-agent ./cmd/insylus-agent
go run ./cmd/insylus-server -listen :8080 -db ./insylus.db -agent-binary ./dist/insylus-agent
```

## Install from Git

The normal install flow is:

```bash
git clone https://github.com/tpersp/Insylus.git
cd Insylus
sudo bash install
```

The installer builds the binaries, installs the `systemd` service, and starts Insylus. If the checkout is exactly on a Git release tag such as `v2026.04.19`, the installer stamps that version into the server binary so the Server Update plugin can compare the installed controller against GitHub releases. If the checkout is on a branch or an untagged commit, the server version is `dev`.

To install a specific release from source:

```bash
git clone https://github.com/tpersp/Insylus.git
cd Insylus
git fetch --tags
git checkout v2026.04.19
sudo bash install
```

You can also override the stamped server version explicitly:

```bash
sudo env INSYLUS_SERVER_VERSION=2026.04.19 bash install
```

## Build binaries manually

```bash
go build ./cmd/insylus-server
go build ./cmd/insylus-agent
go build ./cmd/insylusctl
```

Manual `go build` commands identify the controller/server as `dev` unless you stamp a version yourself. To produce a manually built server binary that participates in release update checks:

```bash
VERSION=2026.04.19
go build -ldflags "-X insylus/internal/version.ServerVersion=$VERSION" -o dist/insylus-server ./cmd/insylus-server
```

## Install as a system service

After building binaries manually, install the persistent `systemd` service:

```bash
sudo bash ./scripts/install-insylus-service.sh
```

This will:

- create a dedicated `insylus` system user for the controller service
- install the binaries under `/opt/insylus/bin`
- store the SQLite database under `/var/lib/insylus/insylus.db`
- enable and start `insylus.service`

Those are defaults, not product requirements. For a different controller layout, set installer environment variables before running the script:

```bash
sudo env \
  INSYLUS_INSTALL_ROOT=/srv/insylus \
  INSYLUS_DATA_DIR=/var/lib/insylus \
  INSYLUS_LISTEN_ADDR=:8080 \
  INSYLUS_APP_USER=insylus \
  INSYLUS_APP_GROUP=insylus \
  INSYLUS_MANAGED_USER=bob \
  INSYLUS_MANAGED_GROUPS=adm,systemd-journal \
  bash ./scripts/install-insylus-service.sh
```

Key distinction:
- `INSYLUS_APP_USER` = local service account for running insylus-server on this controller
- `INSYLUS_MANAGED_USER` = remote account created on enrolled devices for managed SSH access

After installation, the remote managed user and audit groups can be changed from the Settings page. Those values are stored in the controller database and override the install-time defaults.

## Server updates

The Server Update plugin checks the latest GitHub release for the controller/server version. Source-built installs show `dev` unless the server binary was built with `ServerVersion` stamped as shown above.

For an in-app server update to apply successfully, the GitHub release must include these assets:

- `insylus-server`
- `insylus-server-<tag>.sha256`

For example, release tag `v2026.04.19` should include:

- `insylus-server`
- `insylus-server-v2026.04.19.sha256`

If release assets are not present, the update page can detect the latest release but cannot install it automatically.

To remove the service:

```bash
sudo bash ./scripts/uninstall-insylus-service.sh
```

## Useful service commands

Check current service status:

```bash
systemctl status --no-pager insylus.service
```

Follow live logs:

```bash
journalctl -u insylus.service -f
```

Restart after rebuilding or changing configuration:

```bash
sudo systemctl restart insylus.service
```

Stop the service:

```bash
sudo systemctl stop insylus.service
```

Start the service again:

```bash
sudo systemctl start insylus.service
```

Disable automatic startup:

```bash
sudo systemctl disable insylus.service
```

Re-enable automatic startup:

```bash
sudo systemctl enable insylus.service
```

## Host install flow

1. Open the Insylus web UI.
2. Create a device record.
3. Open the device page and copy the one-liner.
4. Run it on the target Linux host with sudo.

The agent will register using the bootstrap token, install `/usr/local/bin/insylus-agent`, write `/etc/insylus-agent/config.json`, and enable `insylus-agent.service`.

Those remote-agent paths are configurable per host before running the one-liner:

```bash
sudo env \
  INSYLUS_AGENT_BIN_PATH=/usr/local/bin/insylus-agent \
  INSYLUS_AGENT_CONFIG_PATH=/etc/insylus-agent/config.json \
  INSYLUS_AGENT_SERVICE_NAME=insylus-agent.service \
  INSYLUS_AGENT_UNIT_PATH=/etc/systemd/system/insylus-agent.service \
  bash -c '<paste install command here>'
```

Newly enrolled devices default to `inventory-only` mode. In that mode the agent reports health, topology, workloads, and update status, but it does not modify users, SSH keys, sudoers, or groups. Switch a device to `access-managed` in the web UI only when Insylus should manage the remote account.

If a host install fails partway through, it is safe to rerun the one-liner after the server has been updated.

To remove the agent from a device, see [Uninstall Insylus Agent From Devices](/opt/insylus/docs/UninstallInsylusAgent.md).

The installer is intentionally verbose and will print:

- download progress steps
- registration success
- installed file paths
- created service name
- final service state
- follow-up commands for status and logs

## Managed SSH aliases

Insylus can maintain system-wide SSH aliases on the controller host so the managed agent can run commands like:

```bash
ssh miscserver
```

instead of needing DNS or a raw IP address.

The managed alias sync writes:

- `/etc/ssh/ssh_config.d/insylus.conf`
- `/etc/ssh/ssh_known_hosts_insylus`

and maps each enabled device to:

- the device friendly name
- a lowercase alias of the same name
- the first reported device IP
- the managed account user
- the configured controller-host identity file
- `StrictHostKeyChecking accept-new` to avoid the interactive first-connect prompt

Only `access-managed` devices with enabled non-disabled access are included in managed SSH aliases. `inventory-only` devices remain visible in inventory but are not treated as managed SSH targets.

Install the managed SSH sync service:

```bash
sudo bash ./scripts/install-insylus-ssh-sync.sh
```

## API and CLI

Read-only inventory API endpoints:

```bash
curl http://127.0.0.1:8080/api/devices
curl http://127.0.0.1:8080/api/devices/<device-id>
curl "http://127.0.0.1:8080/api/devices/find?q=MiscServer"
curl "http://127.0.0.1:8080/api/devices?view=full"
```

Local CLI on the controller host:

```bash
/opt/insylus/bin/insylusctl devices
/opt/insylus/bin/insylusctl devices --json
/opt/insylus/bin/insylusctl devices --find MiscServer --json
/opt/insylus/bin/insylusctl devices --find MiscServer --json --full
/opt/insylus/bin/insylusctl devices --id <device-id> --json
```

The installer also places `insylusctl` on the normal command path via `/usr/local/bin/insylusctl`, so you can usually run:

```bash
insylusctl devices
insylusctl devices --json
insylusctl devices --find MiscServer --json
```

It also installs `insylus` as a shorter alias for the same CLI, so these work too:

```bash
insylus help
insylus devices
insylus devices --json
insylus devices --find MiscServer --json
```

If your current shell says `command not found` right after installation, refresh the shell command cache once:

```bash
hash -r
```

The API and CLI now use tiered structured output:

- compact list output is the default JSON shape for inventory scans
- compact list output includes `name`, `hostname`, `ips`, and `purpose`
- `--find` and `/api/devices/find` return an `info` view by default
- `--full` or `view=full` returns workloads, warnings, and the full health/policy detail
- `info` and `full` include `agent_auto_update` status when available
- `info` and `full` include `device_mode`, either `inventory-only` or `access-managed`

Typical usage:

```bash
insylusctl devices --json
insylusctl devices --find MiscServer --json
insylusctl devices --find MiscServer --json --full
```

## Agent auto-update

Insylus can optionally let enrolled agents update themselves from the controller-served agent binaries.

- auto-update is disabled by default
- the settings page has a global default
- each device page can inherit, enable, or disable auto-update
- updates are checksum-verified and applied by atomic binary replacement
- agents update only when the server agent version is newer than the running agent version
- existing agents must be manually reinstalled once to receive the auto-update-capable agent

## Device modes

`inventory-only` is the safe default for new devices. Use it for controller hosts like Atlas, appliances, or machines that should be visible in Insylus but not modified by access enforcement.

`access-managed` enables managed-account policy enforcement. When a device is first switched to `access-managed`, it starts with access disabled; choose `audit` or `sudo` separately. Switching back to `inventory-only` removes only Insylus-owned access artifacts and does not delete or lock the configured managed user. Existing installs default to managed user `insylus` and audit groups `adm,systemd-journal`; new/systemd installs can set `INSYLUS_MANAGED_USER` and `INSYLUS_MANAGED_GROUPS` before running the installer, and the Settings page can change the persisted controller defaults later.

## Device naming

- Friendly device names preserve the exact casing you enter, so names like `MiscServer` work.
- Letters and `-` are supported for your intended naming style.
- Device names must be unique.
- Uniqueness is case-insensitive, so `MiscServer` and `miscserver` are treated as duplicates.

## Notes

- v1 assumes Debian or Ubuntu style hosts with `systemd`.
- `audit` mode uses the configured managed groups for log visibility. The default is `adm` and `systemd-journal`.
- `disabled` inside `access-managed` currently locks the managed account and removes Insylus-managed privilege files instead of deleting the account entirely.
