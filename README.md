# Insylus

Insylus is a small access inventory for servers, humans, services, and AI agents.

The app answers one core question:

> Where does this principal have local server access, and what sudo level does it have?

This rewrite starts intentionally narrow:

- Web UI
- JSON API
- CLI
- SQLite database
- Servers
- Principals, including AI agents
- Access grants with account name and sudo level

It does not manage remote accounts, install agents, monitor hosts, discover topology, or auto-update devices yet. Those features can be added later when they clearly earn their place.

## Run

```bash
go run ./cmd/insylus serve --db ./insylus.db --listen :8097
```

Open <http://localhost:8097>.

## Install As A Service

From the repo root:

```bash
./install
```

Do not run it with `sudo`; it will ask for sudo only when needed.

The installer handles the normal service setup:

- uses Go to test and build Insylus when Go is installed
- otherwise downloads the latest `insylus-linux-amd64` release binary from GitHub
- installs `/opt/insylus/bin/insylus`
- creates/uses the `insylus` service account
- writes and starts `insylus.service`
- installs `insylus` and `insylusctl` command shims
- verifies the local API

Minimum install requirements:

- `bash`
- `sudo`
- `systemd`
- either `go` or `curl`/`wget` for downloading the release binary

Optional environment overrides:

```bash
INSYLUS_LISTEN_ADDR=:8081 \
INSYLUS_INSTALL_ROOT=/opt/insylus \
INSYLUS_DATA_DIR=/var/lib/insylus \
./install
```

Install a specific release binary when Go is not installed:

```bash
INSYLUS_VERSION=v2026.06.06 ./install
```

## CLI Examples

```bash
insylus server add --name atlas --host atlas.local --addr 10.0.0.5
insylus principal add --name codex --kind ai-agent
insylus access grant --server atlas --principal codex --account aia --sudo passwordless
insylus access list
```

CLI commands use the local API at `http://127.0.0.1:8097` by default. Use `--api URL` or `INSYLUS_API` for a remote controller.

Use `--db PATH` or `INSYLUS_DB` only when you intentionally want to bypass the API and edit a SQLite database directly.

Full documentation:

- [CLI Guide](docs/CLI.md)
- [API Guide](docs/API.md)

## API

- `GET /api/servers`
- `POST /api/servers`
- `PUT /api/servers`
- `GET /api/principals`
- `POST /api/principals`
- `PUT /api/principals`
- `GET /api/access`
- `POST /api/access`
- `PUT /api/access`

POST bodies and responses are JSON.
