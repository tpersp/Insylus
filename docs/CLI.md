# Insylus CLI Guide

The `insylus` command manages the same inventory exposed by the web UI and API.

By default, CLI commands talk to the local controller API at:

```bash
http://127.0.0.1:8097
```

For a remote controller, pass `--api`:

```bash
insylus access list --api http://10.10.10.29:8097
```

Or set:

```bash
export INSYLUS_API=http://10.10.10.29:8097
```

Use `--db PATH` only when intentionally bypassing the API and editing a SQLite database directly.

## Concepts

**Server** is a machine or host you want to track.

**Principal** is who or what has access. This can be a human, service, or AI agent.

**Account** is the local username used on a specific server.

**Access grant** connects a server, principal, local account, and sudo level.

Example:

```text
codex has access to atlas as account aia with passwordless sudo
```

## Sudo Levels

Supported sudo values:

- `none`
- `prompted`
- `passwordless`

## Servers

Add a server:

```bash
insylus server add --name atlas --host atlas.local --addr 10.0.0.5 --notes "controller"
```

List servers:

```bash
insylus server list
```

List servers as JSON:

```bash
insylus server list --json
```

Update a server:

```bash
insylus server update --id 1 --name atlas --host atlas.local --addr 10.0.0.5 --notes "main controller"
```

Updates require `--id`. Use `server list --json` to find it.

## Principals

Add an AI agent:

```bash
insylus principal add --name codex --kind ai-agent --notes "default coding agent"
```

Add a human:

```bash
insylus principal add --name doden --kind human
```

Add a service:

```bash
insylus principal add --name backup-job --kind service
```

List principals:

```bash
insylus principal list
```

List principals as JSON:

```bash
insylus principal list --json
```

Update a principal:

```bash
insylus principal update --id 1 --name codex --kind ai-agent --notes "local coding agent"
```

Supported principal kinds:

- `human`
- `service`
- `ai-agent`

## Access Grants

Grant access:

```bash
insylus access grant --server atlas --principal codex --account aia --sudo passwordless
```

Grant access for a human through a shared/default local account:

```bash
insylus access grant --server raspberrypi --principal doden --account pi --sudo prompted
```

List access:

```bash
insylus access list
```

List access as JSON:

```bash
insylus access list --json
```

Update access:

```bash
insylus access update --id 1 --server atlas --principal codex --account aia --sudo prompted
```

Access updates require `--id`. Use `access list --json` to find it.

## Typical Workflow

Create a server:

```bash
insylus server add --name atlas --host atlas.local --addr 10.0.0.5
```

Create a principal:

```bash
insylus principal add --name codex --kind ai-agent
```

Record access:

```bash
insylus access grant --server atlas --principal codex --account aia --sudo passwordless
```

Inspect the overview:

```bash
insylus access list
```

## JSON Output

Use `--json` for automation:

```bash
insylus access list --json
```

Example output:

```json
[
  {
    "id": 1,
    "server_id": 1,
    "server_name": "atlas",
    "principal_id": 1,
    "principal_name": "codex",
    "account": "aia",
    "sudo": "passwordless",
    "created_at": "2026-06-06T13:09:30Z",
    "updated_at": "2026-06-06T13:09:30Z"
  }
]
```

## Remote Use

Run a single command against a remote controller:

```bash
insylus access list --api http://10.10.10.29:8097
```

Set a default remote controller:

```bash
export INSYLUS_API=http://10.10.10.29:8097
insylus access list
```

## Direct Database Mode

For local development only:

```bash
insylus access list --db ./insylus.db
```

Installed production usage should normally go through the API, because the service database is owned by the `insylus` service account.
