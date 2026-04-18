## Jellyfin plugin

Jellyfin is a free software media system. Insylus queries it through user-provided API keys to show library items, watched status, and resume points.

### Rules

- Jellyfin API keys are not permission-scoped — a key grants full access to the server.
- Library queries always use the default user ID stored in the Jellyfin token configuration; there is no per-command user override.

### CLI commands

Jellyfin query (device is Jellyfin server name, hostname, IP, or device ID):

```bash
insylusctl jellyfin --device <name-or-host-or-ip> --list [--json]
insylusctl jellyfin --device <name-or-host-or-ip> --movies [--json]
insylusctl jellyfin --device <name-or-host-or-ip> --series [--json]
insylusctl jellyfin --device <name-or-host-or-ip> --episodes [--json]
insylusctl jellyfin --device <name-or-host-or-ip> --latest [--json]
insylusctl jellyfin --device <name-or-host-or-ip> --resume [--json]
```

Token management:

```bash
insylusctl jellyfin set-token --device <device> --api-key <key> [--server-name <name>] [--api-url <url>] [--tls-insecure]
insylusctl jellyfin list-tokens [--json]
insylusctl jellyfin remove-token --device <device>
```

### API endpoints

```bash
GET  /api/jellyfin/servers
POST /api/jellyfin/tokens
POST /api/jellyfin/tokens/delete/<device_id>
GET  /api/jellyfin/<device_id>/libraries
GET  /api/jellyfin/<device_id>/items
GET  /api/jellyfin/<device_id>/movies
GET  /api/jellyfin/<device_id>/series
GET  /api/jellyfin/<device_id>/episodes
GET  /api/jellyfin/<device_id>/latest
GET  /api/jellyfin/<device_id>/resume
GET  /api/jellyfin/<device_id>/items/<item_id>
```

### Web UI

- `/jellyfin` — configure Jellyfin API tokens and view configured servers.

### Interpretation

- `--list` shows movies and series together, sorted by name.
- `--movies` and `--series` show items of that type only.
- `--episodes` shows all episodes sorted by series, season, episode number.
- `--latest` shows the 20 most recently added items.
- `--resume` shows in-progress items for the configured default user.
- JSON output includes `series_name` for episodes and resume items.
- Progress percentage is calculated from playback position vs. total runtime.
- User data (watched status, resume points) is always sourced from the configured default user ID.