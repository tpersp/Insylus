(function () {
  const graphEl = document.getElementById("topology-graph");
  const statusEl = document.getElementById("topology-status");
  const selectionEl = document.getElementById("topology-selection");
  if (!graphEl) return;

  const state = {
    graph: { nodes: [], links: [] },
    positions: new Map(),
    hiddenSources: new Set(),
    hiddenKinds: new Set(["service"]),
    viewBox: { x: -120, y: -120, w: 1800, h: 1100 },
    svg: null,
    draggingNode: null,
    panning: null,
  };

  const config = {
    levelGap: 185,
    siblingGap: 46,
    rowGap: 88,
    typeGap: 118,
    rootGap: 130,
    rootRowGap: 170,
    maxRootRowWidth: 2300,
    minSubtree: 160,
    nodeBand: 128,
  };

  fetch("/topology/graph")
    .then((resp) => {
      if (!resp.ok) throw new Error("Topology request failed: " + resp.status);
      return resp.json();
    })
    .then((graph) => {
      state.graph = graph;
      layoutGraph();
      render();
    })
    .catch((err) => {
      setStatus("Could not load topology: " + err.message);
    });

  document.querySelectorAll("[data-topology-filter]").forEach((input) => {
    input.addEventListener("change", () => {
      const source = input.getAttribute("data-topology-filter");
      if (input.checked) state.hiddenSources.delete(source);
      else state.hiddenSources.add(source);
      layoutGraph();
      render();
    });
  });

  document.querySelectorAll("[data-topology-kind-filter]").forEach((input) => {
    const kind = input.getAttribute("data-topology-kind-filter");
    if (!input.checked) state.hiddenKinds.add(kind);
    input.addEventListener("change", () => {
      if (input.checked) state.hiddenKinds.delete(kind);
      else state.hiddenKinds.add(kind);
      layoutGraph();
      render();
    });
  });

  document.querySelectorAll("[data-topology-action]").forEach((button) => {
    button.addEventListener("click", () => {
      const action = button.getAttribute("data-topology-action");
      if (action === "fit") fitView();
      if (action === "save-layout") saveLayout();
      if (action === "reset-layout") resetSavedLayout();
      if (action === "reset") {
        layoutGraph();
        render();
      }
    });
  });

  function setStatus(text) {
    if (statusEl) statusEl.textContent = text;
  }

  function layoutGraph() {
    state.positions.clear();
    const model = graphModel();
    const dims = new Map();
    model.roots.forEach((id) => measureSubtree(id, model.children, model.nodesByID, dims, new Set()));

    const rows = [];
    let row = { roots: [], width: 0, height: 0 };
    model.roots.forEach((id) => {
      const dim = dims.get(id) || { w: config.minSubtree, h: config.nodeBand };
      const nextWidth = row.roots.length ? row.width + config.rootGap + dim.w : dim.w;
      if (row.roots.length && nextWidth > config.maxRootRowWidth) {
        rows.push(row);
        row = { roots: [], width: 0, height: 0 };
      }
      row.roots.push(id);
      row.width = row.roots.length === 1 ? dim.w : row.width + config.rootGap + dim.w;
      row.height = Math.max(row.height, dim.h);
    });
    if (row.roots.length) rows.push(row);

    let y = 0;
    rows.forEach((rootRow) => {
      let x = 0;
      rootRow.roots.forEach((id) => {
        const dim = dims.get(id) || { w: config.minSubtree, h: config.nodeBand };
        placeSubtree(id, x, y, dim.w, model.children, model.nodesByID, dims, new Set());
        x += dim.w + config.rootGap;
      });
      y += rootRow.height + config.rootRowGap;
    });
    applySavedPositions(model.nodes);
    fitView();
  }

  function applySavedPositions(nodes) {
    nodes.forEach((node) => {
      if (!node.position) return;
      const x = Number(node.position.x);
      const y = Number(node.position.y);
      if (!Number.isFinite(x) || !Number.isFinite(y)) return;
      state.positions.set(node.id, { x, y });
    });
  }

  function graphModel() {
    const nodes = visibleNodes();
    const nodesByID = new Map(nodes.map((node) => [node.id, node]));
    const children = new Map();
    const incoming = new Map();

    visibleTreeLinks(nodesByID).forEach((link) => {
      if (!children.has(link.from)) children.set(link.from, []);
      children.get(link.from).push(link.to);
      incoming.set(link.to, link.from);
    });

    children.forEach((items) => {
      items.sort((a, b) => compareNodes(nodesByID.get(a), nodesByID.get(b)));
    });

    const infrastructure = nodes.filter((node) => isInfrastructure(node));
    const rootDevices = nodes.filter((node) => node.kind === "device" && !incoming.has(node.id));
    const rootOther = nodes.filter((node) => node.kind !== "device" && !isInfrastructure(node) && !incoming.has(node.id));
    const roots = [...infrastructure, ...rootDevices, ...rootOther]
      .filter((node) => node.kind !== "group")
      .sort(compareNodes)
      .map((node) => node.id);

    return { nodes, nodesByID, children, roots };
  }

  function visibleTreeLinks(nodesByID) {
    return visibleLinks().filter((link) => {
      if (!nodesByID.has(link.from) || !nodesByID.has(link.to)) return false;
      if (link.id.startsWith("parent:") || link.id.startsWith("workload-link:")) return true;
      const from = nodesByID.get(link.from);
      const to = nodesByID.get(link.to);
      return link.source === "manual" && isInfrastructure(from) && to.kind === "device";
    });
  }

  function measureSubtree(id, children, nodesByID, dims, seen) {
    if (seen.has(id)) return { w: config.minSubtree, h: config.nodeBand };
    seen.add(id);
    const node = nodesByID.get(id);
    if (!node) return { w: config.minSubtree, h: config.nodeBand };
    const grouped = groupedChildren(children.get(id) || [], nodesByID);
    let childWidth = 0;
    let childHeight = 0;
    grouped.forEach((group) => {
      const rows = chunkItems(group.items, maxColumnsFor(group.type));
      rows.forEach((items, rowIndex) => {
        let rowWidth = 0;
        let rowHeight = 0;
        items.forEach((childID, index) => {
          const childDim = measureSubtree(childID, children, nodesByID, dims, new Set(seen));
          rowWidth += childDim.w;
          rowHeight = Math.max(rowHeight, childDim.h);
          if (index < items.length - 1) rowWidth += config.siblingGap;
        });
        childWidth = Math.max(childWidth, rowWidth);
        childHeight += rowHeight;
        if (rowIndex < rows.length - 1) childHeight += config.rowGap;
      });
      childHeight += config.typeGap;
    });
    if (grouped.length) childHeight -= config.typeGap;
    const dim = {
      w: Math.max(nodeSlotWidth(node), childWidth, config.minSubtree),
      h: Math.max(config.nodeBand, grouped.length ? config.levelGap + childHeight : config.nodeBand),
    };
    dims.set(id, dim);
    return dim;
  }

  function placeSubtree(id, x, y, width, children, nodesByID, dims, seen) {
    if (seen.has(id)) return;
    seen.add(id);
    const node = nodesByID.get(id);
    if (!node) return;
    const centerX = x + width / 2;
    state.positions.set(id, { x: centerX, y });

    const grouped = groupedChildren(children.get(id) || [], nodesByID);
    let childY = y + config.levelGap;
    grouped.forEach((group) => {
      const rows = chunkItems(group.items, maxColumnsFor(group.type));
      rows.forEach((items) => {
        const rowWidth = items.reduce((sum, childID, index) => {
          const childDim = dims.get(childID) || { w: config.minSubtree, h: config.nodeBand };
          return sum + childDim.w + (index ? config.siblingGap : 0);
        }, 0);
        const rowHeight = items.reduce((max, childID) => {
          const childDim = dims.get(childID) || { w: config.minSubtree, h: config.nodeBand };
          return Math.max(max, childDim.h);
        }, 0);
        let childX = centerX - rowWidth / 2;
        items.forEach((childID) => {
          const childDim = dims.get(childID) || { w: config.minSubtree, h: config.nodeBand };
          placeSubtree(childID, childX, childY, childDim.w, children, nodesByID, dims, new Set(seen));
          childX += childDim.w + config.siblingGap;
        });
        childY += rowHeight + config.rowGap;
      });
      childY += config.typeGap - config.rowGap;
    });
  }

  function groupedChildren(childIDs, nodesByID) {
    const groups = new Map();
    childIDs.forEach((id) => {
      const node = nodesByID.get(id);
      if (!node) return;
      const type = nodeFilterKind(node);
      if (!groups.has(type)) groups.set(type, []);
      groups.get(type).push(id);
    });
    return typeOrder()
      .filter((type) => groups.has(type))
      .map((type) => ({ type, items: groups.get(type).sort((a, b) => compareNodes(nodesByID.get(a), nodesByID.get(b))) }));
  }

  function render() {
    graphEl.textContent = "";
    const svg = el("svg", { viewBox: viewBoxString(), role: "img" });
    const edgeLayer = el("g", { class: "topology-edge-layer" });
    const nodeLayer = el("g", { class: "topology-node-layer" });
    svg.appendChild(edgeLayer);
    svg.appendChild(nodeLayer);
    state.svg = svg;
    graphEl.appendChild(svg);

    const nodes = visibleNodes();
    const links = visibleLinks().filter((link) => state.positions.has(link.from) && state.positions.has(link.to));
    const hiddenServiceText = state.hiddenKinds.has("service") ? " services hidden" : "";
    setStatus(nodes.length + " shown nodes, " + links.length + " shown links" + hiddenServiceText);

    links.forEach((link) => {
      const from = state.positions.get(link.from);
      const to = state.positions.get(link.to);
      if (!from || !to) return;
      const fromNode = nodes.find((node) => node.id === link.from);
      const toNode = nodes.find((node) => node.id === link.to);
      const path = el("path", {
        d: linkPath(from, to, fromNode, toNode),
        class: "topology-edge source-" + sourceClass(link.source),
      });
      path.addEventListener("click", (event) => {
        event.stopPropagation();
        selectLink(link);
      });
      edgeLayer.appendChild(path);
    });

    nodes.forEach((node) => {
      const pos = state.positions.get(node.id);
      if (!pos) return;
      const type = nodeFilterKind(node);
      const group = el("g", {
        class: "topology-node kind-" + kindClass(node.kind) + " type-" + kindClass(type) + " source-" + sourceClass(node.source),
        transform: "translate(" + pos.x + " " + pos.y + ")",
        tabindex: "0",
      });
      const radius = nodeRadius(node);
      group.appendChild(el("circle", { r: radius }));
      const label = el("text", { y: radius + 17, "text-anchor": "middle" });
      label.textContent = shortText(node.label, labelLength(type));
      group.appendChild(label);
      const meta = nodeMeta(node);
      if (meta) {
        const metaText = el("text", { y: radius + 32, "text-anchor": "middle", class: "topology-node-meta" });
        metaText.textContent = shortText(meta, 20);
        group.appendChild(metaText);
      }
      group.addEventListener("pointerdown", (event) => startNodeDrag(event, node));
      group.addEventListener("click", (event) => {
        event.stopPropagation();
        selectNode(node);
      });
      group.addEventListener("dblclick", (event) => {
        event.stopPropagation();
        if (node.url) window.location.href = node.url;
      });
      nodeLayer.appendChild(group);
    });

    svg.addEventListener("pointerdown", startPan);
    svg.addEventListener("pointermove", onPointerMove);
    svg.addEventListener("pointerup", endPointerAction);
    svg.addEventListener("pointerleave", endPointerAction);
    svg.addEventListener("wheel", onWheel, { passive: false });
  }

  function visibleNodes() {
    return (state.graph.nodes || []).filter((node) => {
      if (node.kind === "group") return false;
      return !state.hiddenSources.has(String(node.source)) && !state.hiddenKinds.has(nodeFilterKind(node));
    });
  }

  function visibleLinks() {
    const nodeSet = new Set(visibleNodes().map((node) => node.id));
    return (state.graph.links || []).filter((link) => {
      return !state.hiddenSources.has(String(link.source)) && nodeSet.has(link.from) && nodeSet.has(link.to);
    });
  }

  function linkPath(from, to, fromNode, toNode) {
    const fromRadius = fromNode ? nodeRadius(fromNode) : 30;
    const toRadius = toNode ? nodeRadius(toNode) : 30;
    const startY = from.y + fromRadius + 12;
    const endY = to.y - toRadius - 12;
    if (endY <= startY) {
      const startX = from.x + fromRadius + 12;
      const endX = to.x - toRadius - 12;
      const midX = startX + (endX - startX) / 2;
      return "M " + startX + " " + from.y + " H " + midX + " V " + to.y + " H " + endX;
    }
    const midY = startY + (endY - startY) / 2;
    return "M " + from.x + " " + startY + " V " + midY + " H " + to.x + " V " + endY;
  }

  function selectNode(node) {
    if (!selectionEl) return;
    const bits = [nodeFilterKind(node), node.source];
    if (node.device_type) bits.push(node.device_type);
    if (node.purpose && node.purpose !== "unknown") bits.push(node.purpose);
    selectionEl.textContent = node.label + " - " + bits.join(" / ") + (node.note ? " - " + node.note : "");
  }

  function selectLink(link) {
    if (!selectionEl) return;
    selectionEl.textContent = link.source + " connection: " + endpointLabel(link.from) + " -> " + endpointLabel(link.to) + (link.label ? " - " + link.label : "");
  }

  function saveLayout() {
    const positions = [];
    visibleNodes().forEach((node) => {
      const pos = state.positions.get(node.id);
      if (!pos) return;
      positions.push({ id: node.id, x: pos.x, y: pos.y });
    });
    fetch("/topology/layout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ positions }),
    })
      .then((resp) => {
        if (!resp.ok) throw new Error("Save failed: " + resp.status);
        (state.graph.nodes || []).forEach((node) => {
          const pos = state.positions.get(node.id);
          if (pos) node.position = { x: pos.x, y: pos.y };
        });
        setStatus("Saved " + positions.length + " node positions");
      })
      .catch((err) => setStatus(err.message));
  }

  function resetSavedLayout() {
    fetch("/topology/layout/reset", { method: "POST" })
      .then((resp) => {
        if (!resp.ok) throw new Error("Reset failed: " + resp.status);
        (state.graph.nodes || []).forEach((node) => {
          delete node.position;
        });
        layoutGraph();
        render();
        setStatus("Layout reset");
      })
      .catch((err) => setStatus(err.message));
  }

  function endpointLabel(id) {
    const node = (state.graph.nodes || []).find((item) => item.id === id);
    return node ? node.label : id;
  }

  function startNodeDrag(event, node) {
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    state.draggingNode = { id: node.id, last: screenPoint(event) };
  }

  function startPan(event) {
    state.panning = { last: screenPoint(event) };
  }

  function onPointerMove(event) {
    if (state.draggingNode) {
      const now = screenPoint(event);
      const delta = screenDeltaToView(deltaPoint(state.draggingNode.last, now));
      const pos = state.positions.get(state.draggingNode.id);
      pos.x += delta.x;
      pos.y += delta.y;
      state.draggingNode.last = now;
      render();
      return;
    }
    if (state.panning) {
      const now = screenPoint(event);
      const delta = screenDeltaToView(deltaPoint(state.panning.last, now));
      state.viewBox.x -= delta.x;
      state.viewBox.y -= delta.y;
      state.panning.last = now;
      updateViewBox();
    }
  }

  function endPointerAction() {
    state.draggingNode = null;
    state.panning = null;
  }

  function onWheel(event) {
    event.preventDefault();
    const factor = event.deltaY > 0 ? 1.12 : 0.88;
    const before = svgPoint(event);
    state.viewBox.w *= factor;
    state.viewBox.h *= factor;
    const after = svgPoint(event);
    state.viewBox.x += before.x - after.x;
    state.viewBox.y += before.y - after.y;
    updateViewBox();
  }

  function fitView() {
    const nodes = visibleNodes();
    if (!nodes.length) {
      state.viewBox = { x: -120, y: -120, w: 1800, h: 1100 };
      updateViewBox();
      return;
    }
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    nodes.forEach((node) => {
      const pos = state.positions.get(node.id);
      if (!pos) return;
      const radius = nodeRadius(node);
      minX = Math.min(minX, pos.x - radius - 100);
      minY = Math.min(minY, pos.y - radius - 72);
      maxX = Math.max(maxX, pos.x + radius + 100);
      maxY = Math.max(maxY, pos.y + radius + 72);
    });
    const pad = 90;
    state.viewBox = {
      x: minX - pad,
      y: minY - pad,
      w: Math.max(maxX - minX + pad * 2, 1200),
      h: Math.max(maxY - minY + pad * 2, 780),
    };
    updateViewBox();
  }

  function updateViewBox() {
    if (state.svg) state.svg.setAttribute("viewBox", viewBoxString());
  }

  function viewBoxString() {
    return [state.viewBox.x, state.viewBox.y, state.viewBox.w, state.viewBox.h].join(" ");
  }

  function svgPoint(event) {
    const rect = state.svg.getBoundingClientRect();
    return {
      x: state.viewBox.x + ((event.clientX - rect.left) / rect.width) * state.viewBox.w,
      y: state.viewBox.y + ((event.clientY - rect.top) / rect.height) * state.viewBox.h,
    };
  }

  function screenPoint(event) {
    return { x: event.clientX, y: event.clientY };
  }

  function deltaPoint(from, to) {
    return { x: to.x - from.x, y: to.y - from.y };
  }

  function screenDeltaToView(delta) {
    const rect = state.svg.getBoundingClientRect();
    return {
      x: (delta.x / rect.width) * state.viewBox.w,
      y: (delta.y / rect.height) * state.viewBox.h,
    };
  }

  function nodeRadius(node) {
    const type = nodeFilterKind(node);
    if (type === "device") return 48;
    if (type === "infrastructure") return 44;
    if (type === "vm") return 38;
    if (type === "lxc" || type === "container") return 31;
    if (type === "service") return 19;
    return 30;
  }

  function nodeSlotWidth(node) {
    const type = nodeFilterKind(node);
    if (type === "device" || type === "infrastructure") return 220;
    if (type === "vm") return 190;
    if (type === "lxc" || type === "container") return 168;
    if (type === "service") return 118;
    return 160;
  }

  function chunkItems(items, maxColumns) {
    const out = [];
    for (let i = 0; i < items.length; i += maxColumns) {
      out.push(items.slice(i, i + maxColumns));
    }
    return out;
  }

  function maxColumnsFor(type) {
    if (type === "service") return 6;
    if (type === "container" || type === "lxc") return 4;
    if (type === "vm") return 4;
    return 3;
  }

  function nodeMeta(node) {
    const type = nodeFilterKind(node);
    if (type === "device") return node.device_type || "bare-metal";
    if (type === "vm" || type === "lxc" || type === "container") return type;
    if (type === "service") return "service";
    if (type === "infrastructure") return node.kind;
    return type;
  }

  function nodeFilterKind(node) {
    if (node.kind === "workload") return String(node.note || "service");
    if (node.kind === "device") {
      if (node.device_type === "vm") return "vm";
      if (node.device_type === "lxc") return "lxc";
      if (node.device_type === "container") return "container";
      return "device";
    }
    if (isInfrastructure(node)) return "infrastructure";
    return String(node.kind || "device");
  }

  function isInfrastructure(node) {
    return node && ["internet", "router", "switch", "access-point", "patch-panel", "other"].includes(String(node.kind));
  }

  function compareNodes(a, b) {
    const weight = typeOrder().indexOf(nodeFilterKind(a)) - typeOrder().indexOf(nodeFilterKind(b));
    if (weight !== 0) return weight;
    return String(a && a.label || "").localeCompare(String(b && b.label || ""));
  }

  function typeOrder() {
    return ["infrastructure", "device", "lxc", "vm", "container", "service"];
  }

  function labelLength(type) {
    if (type === "service") return 14;
    if (type === "container" || type === "lxc") return 18;
    return 22;
  }

  function shortText(value, max) {
    const text = String(value || "");
    if (text.length <= max) return text;
    return text.slice(0, Math.max(1, max - 1)) + "...";
  }

  function kindClass(value) {
    return String(value || "unknown").replace(/[^a-z0-9_-]/gi, "-");
  }

  function sourceClass(value) {
    return String(value || "unknown").replace(/[^a-z0-9_-]/gi, "-");
  }

  function el(name, attrs) {
    const node = document.createElementNS("http://www.w3.org/2000/svg", name);
    Object.entries(attrs || {}).forEach(([key, value]) => {
      node.setAttribute(key, value);
    });
    return node;
  }
})();
