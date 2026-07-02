---
name: insylus
description: Use the Insylus HTTP API to inspect and update server access inventory, especially where humans, services, or AI agents have local accounts and sudo.
metadata: {"openclaw":{"os":["linux","macos"],"requires":{"anyBins":["curl"]},"emoji":"◉"}}
---

# Insylus API Skill

Use this skill when you need to know which servers a principal can access, which local account is used, or whether access has sudo.

This skill is API-first. Do not assume the `insylus` CLI exists locally; agents usually run away from the Insylus controller.

## Required Setup

Set the controller URL:

```bash
export INSYLUS_API="http://<insylus-controller>:8097"
```

Example:

```bash
export INSYLUS_API="http://insylus.example.local:8097"
```

If `INSYLUS_API` is not set, ask the user for the Insylus controller URL. Do not guess or invent a private IP.

## Core Rules

- Query Insylus before guessing server access.
- Treat Insylus as inventory, not proof that SSH currently works.
- Do not assume passwordless sudo unless an access grant says `sudo=passwordless`.
- Do not use the old `insylusctl` device/plugin commands; the new Insylus app exposes a smaller API.
- Use JSON endpoints only.
- Keep writes conservative. If unsure, list current records first.
- The API currently has no auth. Only use trusted controller URLs supplied by the user/environment.

## Concepts

| Term | Meaning |
|------|---------|
| Server | A tracked host or machine |
| Principal | Who or what has access, such as a human, service, or AI agent |
| Account | The local username used on that server |
| Access grant | Server + principal + account + sudo level |

Supported principal kinds:

- `human`
- `service`
- `ai-agent`

Supported sudo levels:

- `none`
- `prompted`
- `passwordless`

## Helper Pattern

Use this shell pattern in commands:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API to the Insylus controller URL}"
```

For readable JSON, use `jq` if available, but do not require it:

```bash
curl -fsS "$BASE/api/access"
```

## Read Inventory

List all servers:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS "$BASE/api/servers"
```

List all principals:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS "$BASE/api/principals"
```

List all access grants:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS "$BASE/api/access"
```

## Answer Common Questions

### Where Does This Principal Have Access?

Without `jq`:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS "$BASE/api/access"
```

Then inspect rows where `principal_name` matches the principal.

With `jq`:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
PRINCIPAL="codex"
curl -fsS "$BASE/api/access" |
  jq --arg p "$PRINCIPAL" '.[] | select(.principal_name == $p)'
```

### Does This Principal Have Passwordless Sudo Anywhere?

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
PRINCIPAL="codex"
curl -fsS "$BASE/api/access" |
  jq --arg p "$PRINCIPAL" '.[] | select(.principal_name == $p and .sudo == "passwordless")'
```

If `jq` is not available, list access and inspect `sudo` manually.

### Who Has Access To A Server?

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
SERVER="atlas"
curl -fsS "$BASE/api/access" |
  jq --arg s "$SERVER" '.[] | select(.server_name == $s)'
```

## Create Records

Create a server:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X POST "$BASE/api/servers" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "atlas",
    "hostname": "atlas.local",
    "address": "192.0.2.5",
    "notes": "controller"
  }'
```

Create a principal:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X POST "$BASE/api/principals" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "codex",
    "kind": "ai-agent",
    "notes": "default coding agent"
  }'
```

Create an access grant:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X POST "$BASE/api/access" \
  -H 'Content-Type: application/json' \
  -d '{
    "server_name": "atlas",
    "principal_name": "codex",
    "account": "aia",
    "sudo": "passwordless"
  }'
```

## Update Records

Updates require an `id`. List records first to find the ID.

Update a server:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X PUT "$BASE/api/servers" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "name": "atlas",
    "hostname": "atlas.local",
    "address": "192.0.2.5",
    "notes": "main controller"
  }'
```

Update a principal:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X PUT "$BASE/api/principals" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "name": "codex",
    "kind": "ai-agent",
    "notes": "local coding agent"
  }'
```

Update an access grant:

```bash
BASE="${INSYLUS_API:?Set INSYLUS_API}"
curl -fsS -X PUT "$BASE/api/access" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "server_name": "atlas",
    "principal_name": "codex",
    "account": "aia",
    "sudo": "prompted"
  }'
```

## API Reference

Read the full API docs when available:

```text
Insylus/docs/API.md
```

Endpoint summary:

```text
GET  /api/servers
POST /api/servers
PUT  /api/servers
DELETE /api/servers?id=ID

GET  /api/principals
POST /api/principals
PUT  /api/principals
DELETE /api/principals?id=ID

GET  /api/access
POST /api/access
PUT  /api/access
DELETE /api/access?id=ID
```

## Response Shapes

Access grant example:

```json
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
```

Error example:

```json
{
  "error": "server name is required"
}
```

## Safety Notes

- The new Insylus app does not install agents or enforce access policy.
- It is an access inventory, not an SSH broker.
- If a principal has `sudo=prompted`, do not assume you can run sudo without a password.
- If a server or principal is missing, ask before creating it unless the task explicitly says to update Insylus.
- Never store secrets in notes.
