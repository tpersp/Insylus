## Docker plugin

The Docker plugin provides container lifecycle control and inspection for configured Docker hosts. Commands are executed over SSH using system SSH configuration, with an optional SSH user stored in the plugin.

### Rules

- Docker management is performed over SSH to the configured host using the `docker` CLI. The host must be reachable by normal SSH from the controller.
- Docker hosts can be standalone plugin targets or linked to an existing Insylus target.
- Discovery of running containers is handled separately by the services discovery system. This plugin adds control and inspection.
- If Docker is not installed on a host, operations will return an error indicating the Docker CLI is unavailable.
- Start, stop, restart, pause, and unpause are non-blocking — they initiate the action and return immediately.

### CLI commands

Host configuration:

```bash
insylusctl docker set-host --host <ssh-host> [--name <name>] [--ssh-user <user>] [--device-id <target-id>]
insylusctl docker list-hosts [--json]
insylusctl docker remove-host --host <name-or-host-or-id>
```

Container listing:

```bash
insylusctl docker --host <host> --list [--json]
insylusctl docker --host <host> --containers [--json]
```

Container inspection:

```bash
insylusctl docker --host <host> --inspect <name> [--json]
insylusctl docker --host <host> --logs <name> [--tail N] [--timestamps] [--json]
insylusctl docker --host <host> --stats <name> [--json]
```

Container lifecycle:

```bash
insylusctl docker --host <host> --start <name>
insylusctl docker --host <host> --stop <name>
insylusctl docker --host <host> --restart <name>
insylusctl docker --host <host> --pause <name>
insylusctl docker --host <host> --unpause <name>
```

Images:

```bash
insylusctl docker --host <host> --images [--json]
```

### API endpoints

```bash
GET  /api/docker/nodes
GET  /api/docker/config
GET  /api/docker/config/<target_id>
POST /api/docker/config
POST /api/docker/config/<target_id>/delete
GET  /api/docker/containers/<target_id>?all=0|1
GET  /api/docker/containers/<target_id>/<name>/inspect
GET  /api/docker/containers/<target_id>/<name>/logs?tail=100&timestamps=false
GET  /api/docker/containers/<target_id>/<name>/stats
POST /api/docker/containers/<target_id>/<name>/start
POST /api/docker/containers/<target_id>/<name>/stop
POST /api/docker/containers/<target_id>/<name>/restart
POST /api/docker/containers/<target_id>/<name>/pause
POST /api/docker/containers/<target_id>/<name>/unpause
GET  /api/docker/images/<target_id>
```

### Web UI

- `/docker` — configure Docker hosts and list configured hosts
- `/docker/devices/<target_id>` — container list and image list for a configured Docker host

### Interpretation

- `--host` first matches a configured Docker host by target ID, name, hostname, or Docker SSH host. If no config matches, legacy device lookup is used.
- `--list` shows only running containers. `--containers` shows all containers including stopped.
- `--logs --tail N` limits output to the last N lines (default 100).
- `--logs --timestamps` prefixes each line with the RFC3339 timestamp.
- Container names in Docker are usually the container-friendly name set at `docker run --name`, not the image name.
- `--inspect` returns detailed information including environment variables, command, mounts, networks, and port mappings.
- `--stats` returns a one-shot CPU percentage and memory usage snapshot.
