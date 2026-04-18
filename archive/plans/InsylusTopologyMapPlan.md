# Insylus Topology Map Plan

Archived 2026-04-18. The web-only topology plugin is implemented under
`plugins/topology`. V1 manual graph support and V2 editing/saved layout support
are complete. Gateway/subnet discovery and vendor integrations remain future
product ideas, not active implementation work.

---

**IMPLEMENTED**

- `topology_nodes` and `topology_links` SQLite tables with full CRUD operations
- `topology_node_positions` SQLite table for saved graph layout
- `/topology` web page with graph UI (Cytoscape.js)
- `/topology/graph` endpoint for UI-private graph payload
- CRUD endpoints for nodes and links, including node renaming and link label edits
- "Save layout" and "Reset layout" actions

**FUTURE IDEAS:**
- Gateway/subnet discovery (agent report changes for gateway IP, interface list, routes)
- Vendor integrations (SNMP, UniFi, Omada, MikroTik, LLDP)

---

## Summary

Build a futureproof topology feature as a phased graph system, starting with a human-readable homelab map that draws itself from existing Insylus device topology and also lets users add missing physical/network gear manually.

Chosen defaults:
- First use case: **homelab map**
- V1 manual editing: **add infrastructure gear and links**
- V3/V4 discovery: **gateway/subnet inference first, vendor integrations later**
- UI approach: server-rendered Insylus pages plus a bundled static graph script, no frontend build system required.

## Key Changes

### V1: Topology Map + Manual Gear

Add a `/topology` web page with an interactive graph.

Graph nodes:
- Enrolled Insylus devices from existing records
- Workloads from discovery snapshots, displayed as smaller child nodes
- Manual infrastructure nodes: `internet`, `router`, `switch`, `access-point`, `patch-panel`, `other`
- Synthetic grouping nodes for `unknown parent` and `no parent` only if useful for readability

Graph links:
- Existing inferred/manual device parent relationships
- Device-to-workload links from discovery
- Manual links between devices and infrastructure nodes
- Link source must be visible: `manual`, `inferred`, or `discovered`

Add persistent manual topology tables:
- `topology_nodes`: id, name, kind, note, created_at, updated_at
- `topology_links`: id, from_kind, from_id, to_kind, to_id, label, source, created_at, updated_at
- `from_kind`/`to_kind` values: `device`, `topology_node`
- V1 only writes `source='manual'` through user actions; inferred/discovered links are generated from existing records.

Add server support:
- `GET /topology` renders the page
- `/topology/graph` returns the UI-private graph payload for the web map
- `POST /topology/nodes` creates manual infrastructure nodes
- `POST /topology/links` creates manual links
- `POST /topology/links/{id}/delete` deletes manual links
- `POST /topology/nodes/{id}/delete` deletes manual nodes only if no links reference them, or deletes their manual links in the same transaction with clear UI copy

Use a static bundled graph library, preferably Cytoscape.js as a single vendored file under embedded static assets. Do not introduce npm, React, or a frontend build pipeline.

V1 UI behavior:
- Add “Topology” to the header nav.
- Left/top controls: add infrastructure node, add link, filter by type/source.
- Main graph: pan, zoom, drag for temporary layout adjustment.
- V1 does not need saved node positions.
- Clicking a device node opens its existing device detail page.
- Clicking manual nodes/links exposes basic note/label/delete controls.
- If graph data cannot load, show a useful error and keep the rest of the page usable.

### V2: Better Editing + Stable Layout

Add saved layout positions:
- `topology_node_positions`: subject_kind, subject_id, x, y, updated_at
- Applies to both devices and manual topology nodes.
- Save positions from the graph with a “Save layout” button, not continuously on every drag.

Add editing:
- Rename/update manual infrastructure nodes.
- Edit manual link labels.
- Add optional link metadata fields: medium/type such as `ethernet`, `wifi`, `virtual`, `unknown`.
- Add a “Reset layout” action that clears saved positions but does not delete nodes or links.

Improve graph rendering:
- Cluster by parent/host where possible.
- Show Proxmox and Docker hosts as natural grouping anchors.
- Keep unknown-parent devices visible instead of hiding them.
- Keep layout deterministic when no saved positions exist.

### V3: Gateway/Subnet Discovery

Extend agent/server discovery conservatively.

Agent report additions:
- Default gateway IP where available
- Interface list with interface name, IP/CIDR, MAC if available
- Optional route summary
- Do not collect secrets or sensitive network config.

Server inference:
- Create discovered/synthetic network nodes for subnets and probable gateways.
- Infer `device -> subnet` links from reported IP/CIDR.
- Infer `subnet -> gateway` links when multiple devices report the same gateway.
- Mark all such links as `discovered` or `inferred`; never overwrite manual links.
- If a manual router node matches a discovered gateway IP/name later, surface it as a suggested merge/link, not an automatic destructive merge.

UI additions:
- Show subnet/gateway suggestions in the topology page.
- Let users accept suggested links or ignore them.
- Keep manual topology as the source of truth when it conflicts with inferred topology.

### V4: Optional Network Integrations

Add integration-based discovery after the core graph model is stable.

Likely integrations:
- SNMP for switches/routers
- UniFi, Omada, MikroTik, or other controller APIs
- LLDP data where available from Linux hosts

Design constraints:
- Integrations must be optional and disabled by default.
- Store credentials/settings separately from topology graph data.
- Integration output creates suggested or discovered nodes/links, never direct manual edits.
- Vendor-specific data should normalize into the same graph payload used by V1-V3.

## API And Data Shape

Add a new graph payload type, separate from compact device inventory so `insylusctl devices --json` stays small.

Recommended UI-private `/topology/graph` shape:

```json
{
  "nodes": [
    {
      "id": "device:<device_id>",
      "kind": "device",
      "label": "Atlas",
      "device_id": "<device_id>",
      "device_type": "bare-metal",
      "purpose": "proxmox-node",
      "source": "insylus",
      "url": "/devices/<device_id>",
      "status": {
        "last_seen_at": "...",
        "agent_version": "..."
      }
    },
    {
      "id": "topology_node:<id>",
      "kind": "switch",
      "label": "Basement Switch",
      "source": "manual"
    }
  ],
  "links": [
    {
      "id": "manual:<id>",
      "from": "topology_node:<id>",
      "to": "device:<device_id>",
      "label": "uplink",
      "source": "manual"
    }
  ]
}
```

Rules:
- Graph node ids are prefixed strings and stable across page loads.
- Manual database ids remain internal.
- Existing `/api/devices` output is unchanged.
- Do not add `insylusctl topology`; topology is web UI only. Do not add topology fields to compact device output.
- Update `AGENT_GUIDE.md`, `Insylusv2OutputShapePlan.md`, and `openclaw-skills/insylus` when the public API/CLI shape changes.

## Implementation Notes

Primary implementation areas:
- Server/store: add migrations and store methods for topology nodes/links.
- Server/routes: add topology page and graph JSON/mutation handlers.
- Templates/static assets: add `topology.html`, graph JS, and CSS matching existing Insylus UI.

Store behavior:
- Use additive SQLite migrations in the existing migration style.
- Validate manual node kind against the allowed set.
- Validate link endpoints exist.
- Prevent duplicate exact manual links between the same endpoints unless label differs.
- Deleting an enrolled device must not leave broken manual links; use foreign-key-compatible cleanup or explicit store cleanup for device endpoint links.
- Manual links and nodes must never be modified by discovery.

Graph assembly behavior:
- Build graph from current `ListDevices` records plus manual topology tables.
- Device parent links come from `Resolved.ParentState == linked`.
- Workload nodes are generated from discovery snapshots and are not persisted as manual nodes.
- Manual links can connect device-to-device, manual-to-device, or manual-to-manual.
- If both inferred and manual links exist between the same two subjects, show both only if labels/sources differ; otherwise prefer displaying the manual link.

## Test Plan

Automated tests:
- Migration creates topology tables without breaking existing stores.
- Creating/listing/updating/deleting manual topology nodes works.
- Creating manual links validates endpoint existence and kind.
- Deleting manual nodes cleans or rejects linked records according to chosen V1 behavior.
- `/topology/graph` includes devices, inferred parent links, workloads, manual nodes, and manual links for the web UI.
- Existing `/api/devices` compact/info/full tests continue to pass unchanged.
- Parent topology override tests continue to pass unchanged.

Manual verification:
- Run `go test ./...`.
- Build server and controller binaries.
- Restart `insylus.service` if implementation affects the running web app.
- Visit `/topology`.
- Confirm the map renders with current devices.
- Add a router, switch, and access point.
- Link a device to the switch and the switch to the router.
- Refresh the page and confirm manual topology persists.
- Open a device from the graph and confirm navigation works.
- Confirm `insylusctl devices` and `insylusctl devices --json` remain compact and unchanged.

## Assumptions

- This is a human-facing web UI feature only; do not expose it as a public CLI/API lookup surface.
- V1 should favor usefulness and simplicity over perfect physical accuracy.
- Manual topology is authoritative and must survive all discovery passes.
- Saved graph positions are deferred to V2 to keep V1 smaller.
- Gateway/subnet discovery is V3 because it requires agent report changes and careful privacy/signal decisions.
- Vendor/controller integrations are V4 because they need credential management and integration-specific failure handling.
