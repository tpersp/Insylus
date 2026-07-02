# Insylus API Guide

The Insylus API is JSON over HTTP.

Installed default base URL:

```text
http://127.0.0.1:8097
```

From another machine on the same network, use the controller address, for example:

```text
http://insylus.example.local:8097
```

All request bodies are JSON. All API responses are JSON.

## Concepts

**Server** is a tracked host.

**Principal** is who or what has access: a human, service, or AI agent.

**Access grant** records that a principal can access a server through a local account, with a specific sudo level.

## Data Shapes

### Server

```json
{
  "id": 1,
  "name": "atlas",
  "hostname": "atlas.local",
  "address": "192.0.2.5",
  "notes": "controller",
  "created_at": "2026-06-06T13:09:30Z",
  "updated_at": "2026-06-06T13:09:30Z"
}
```

Required on create/update:

- `name`

Optional:

- `hostname`
- `address`
- `notes`

### Principal

```json
{
  "id": 1,
  "name": "codex",
  "kind": "ai-agent",
  "notes": "default coding agent",
  "created_at": "2026-06-06T13:09:30Z",
  "updated_at": "2026-06-06T13:09:30Z"
}
```

Required on create/update:

- `name`

Optional:

- `kind`, defaults to `ai-agent`
- `notes`

Supported `kind` values:

- `human`
- `service`
- `ai-agent`

### Access Grant

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

Create/update can reference server and principal by name:

```json
{
  "server_name": "atlas",
  "principal_name": "codex",
  "account": "aia",
  "sudo": "passwordless"
}
```

Or by ID:

```json
{
  "server_id": 1,
  "principal_id": 1,
  "account": "aia",
  "sudo": "passwordless"
}
```

Required on create/update:

- server, either `server_name` or `server_id`
- principal, either `principal_name` or `principal_id`
- `account`

Optional:

- `sudo`, defaults to `none`

Supported `sudo` values:

- `none`
- `prompted`
- `passwordless`

## Servers

### List Servers

```bash
curl -s http://127.0.0.1:8097/api/servers
```

Response:

```json
[
  {
    "id": 1,
    "name": "atlas",
    "hostname": "atlas.local",
    "address": "192.0.2.5",
    "notes": "controller",
    "created_at": "2026-06-06T13:09:30Z",
    "updated_at": "2026-06-06T13:09:30Z"
  }
]
```

### Create Server

```bash
curl -s -X POST http://127.0.0.1:8097/api/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "atlas",
    "hostname": "atlas.local",
    "address": "192.0.2.5",
    "notes": "controller"
  }'
```

### Update Server

`PUT /api/servers` updates by `id`.

```bash
curl -s -X PUT http://127.0.0.1:8097/api/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "name": "atlas",
    "hostname": "atlas.local",
    "address": "192.0.2.5",
    "notes": "main controller"
  }'
```

### Delete Server

Deletes by `id`. Related access grants are removed automatically.

```bash
curl -s -X DELETE 'http://127.0.0.1:8097/api/servers?id=1'
```

## Principals

### List Principals

```bash
curl -s http://127.0.0.1:8097/api/principals
```

### Create Principal

```bash
curl -s -X POST http://127.0.0.1:8097/api/principals \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "codex",
    "kind": "ai-agent",
    "notes": "default coding agent"
  }'
```

### Update Principal

`PUT /api/principals` updates by `id`.

```bash
curl -s -X PUT http://127.0.0.1:8097/api/principals \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "name": "codex",
    "kind": "ai-agent",
    "notes": "local coding agent"
  }'
```

### Delete Principal

Deletes by `id`. Related access grants are removed automatically.

```bash
curl -s -X DELETE 'http://127.0.0.1:8097/api/principals?id=1'
```

## Access

### List Access

```bash
curl -s http://127.0.0.1:8097/api/access
```

### Create Access Grant

```bash
curl -s -X POST http://127.0.0.1:8097/api/access \
  -H 'Content-Type: application/json' \
  -d '{
    "server_name": "atlas",
    "principal_name": "codex",
    "account": "aia",
    "sudo": "passwordless"
  }'
```

### Update Access Grant

`PUT /api/access` updates by `id`.

```bash
curl -s -X PUT http://127.0.0.1:8097/api/access \
  -H 'Content-Type: application/json' \
  -d '{
    "id": 1,
    "server_name": "atlas",
    "principal_name": "codex",
    "account": "aia",
    "sudo": "prompted"
  }'
```

### Delete Access Grant

Deletes by `id`.

```bash
curl -s -X DELETE 'http://127.0.0.1:8097/api/access?id=1'
```

## Error Responses

Errors use this shape:

```json
{
  "error": "server name is required"
}
```

Examples:

- `400 Bad Request` for invalid input
- `405 Method Not Allowed` for unsupported HTTP methods
- `500 Internal Server Error` for unexpected server/storage failures

## Automation Flow

A simple automation flow:

1. Create or list servers.
2. Create or list principals.
3. Create access grants using `server_name` and `principal_name`.
4. Use `GET /api/access` for the current access overview.

Example:

```bash
BASE=http://insylus.example.local:8097

curl -s -X POST "$BASE/api/servers" \
  -H 'Content-Type: application/json' \
  -d '{"name":"atlas","hostname":"atlas.local","address":"192.0.2.5"}'

curl -s -X POST "$BASE/api/principals" \
  -H 'Content-Type: application/json' \
  -d '{"name":"codex","kind":"ai-agent"}'

curl -s -X POST "$BASE/api/access" \
  -H 'Content-Type: application/json' \
  -d '{"server_name":"atlas","principal_name":"codex","account":"aia","sudo":"passwordless"}'

curl -s "$BASE/api/access"
```

## Notes

The API currently has no authentication. Run it only on a trusted network or behind a reverse proxy that provides access control.
