# Insylus API Fields

## Server

| Field | Type | Meaning |
|-------|------|---------|
| `id` | integer | Stable server ID |
| `name` | string | Human-friendly server name, unique |
| `hostname` | string | DNS or host name |
| `address` | string | IP address or reachable address |
| `notes` | string | Manual server notes |
| `created_at` | string | RFC3339 creation time |
| `updated_at` | string | RFC3339 update time |

## Principal

| Field | Type | Meaning |
|-------|------|---------|
| `id` | integer | Stable principal ID |
| `name` | string | Human/service/agent name, unique |
| `kind` | string | `human`, `service`, or `ai-agent` |
| `notes` | string | Manual principal notes |
| `created_at` | string | RFC3339 creation time |
| `updated_at` | string | RFC3339 update time |

## Access Grant

| Field | Type | Meaning |
|-------|------|---------|
| `id` | integer | Stable access grant ID |
| `server_id` | integer | Linked server ID |
| `server_name` | string | Linked server name |
| `principal_id` | integer | Linked principal ID |
| `principal_name` | string | Linked principal name |
| `account` | string | Local account used on the server |
| `sudo` | string | `none`, `prompted`, or `passwordless` |
| `created_at` | string | RFC3339 creation time |
| `updated_at` | string | RFC3339 update time |
