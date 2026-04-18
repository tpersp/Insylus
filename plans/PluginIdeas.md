# Insylus Plugin Ideas

A list of potential plugins for the Insylus compile-time plugin architecture. Each entry describes the problem it solves, what it would own, and why it fits as a plugin rather than core.

---



---

## 2. Docker Manager

**Problem:** Docker hosts are already discovered and inventoried, but container-level operations require SSH or direct host access. Starting/stopping/inspecting containers via CLI is useful but scattered.

**What it owns:**
- Container lifecycle (start, stop, restart, pause, unpause)
- Container inspection (logs, stats, port mappings, env vars)
- Image listing and disk usage
- Compose project awareness (if docker-compose is present)

**Plugin shape:**
- `insylusctl docker --host <device> --list`, `--stop <container>`, `--logs <container>`, `--stats <container>`
- `GET /api/docker/<device-id>/containers`, `POST /api/docker/<device-id>/containers/<name>/start`
- Web UI: container list per device, start/stop buttons, log viewer

**Why plugin:** Docker is ubiquitous but not universal — some devices are bare-metal, VMs, or LXC with no Docker at all. A plugin keeps the core lean and lets Docker-heavy users add it cleanly.

**Note:** The services discovery already reports running containers. This plugin adds *control*, not discovery.

---



---

## 4. Log Aggregator

**Problem:** When something breaks, you SSH into devices and grep logs manually. Centralized log collection would make debugging faster but adds complexity.

**What it owns:**
- rsyslog/syslog collection from enrolled devices
- Log storage and indexing (SQLite-backed for simplicity)
- Search interface by device, service, severity, time range
- Retention policy (e.g., 7 days for info, 30 days for errors)

**Plugin shape:**
- `insylusctl logs [--device <device>] [--since <duration>] [--grep <pattern>]`
- `GET /api/logs?device=&since=&grep=`
- Web UI: log viewer with filters, severity coloring

**Why plugin:** Log collection adds a network and storage dependency. Not every homelab wants it — plugin model respects that.

**Out of scope:** Full log analysis or alerting. Search and grep is the MVP scope.

---

## 5. Network Status / Ping Monitor

**Problem:** Devices show "last seen" in Insylus but there's no active monitoring — you only know a device is down when you try to access it and it doesn't respond.

**What it owns:**
- Periodic ping/port checks against enrolled devices and arbitrary IPs
- Uptime history and average availability percentage
- Latency tracking over time
- Configurable check interval (default every 5 min) and timeout

**Plugin shape:**
- `insylusctl monitor --list`, `--status <device>`, `--history <device>`
- `GET /api/monitor`, `GET /api/monitor/<device-id>/history`
- Web UI: device status grid (green/yellow/red), uptime percentages, latency sparklines

**Why plugin:** Active monitoring is a specific workflow, not a core requirement for device inventory and access management. Fits the "nice to have" plugin category.

**Note:** Does not replace external tools like Pingora or Uptime Kuma — it's a lightweight built-in option.

---



---



---



---



---



---

*Feel free to reorder, add, or remove from this list based on what would actually be useful in your homelab.*