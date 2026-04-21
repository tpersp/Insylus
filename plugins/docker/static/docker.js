(function() {
  'use strict';

  const deviceId = window.DOCKER_DEVICE_ID;
  const configAPI = window.DOCKER_CONFIG_API || '/api/docker/config';

  // --- Docker Hosts Page ---
  const nodesBody = document.getElementById('docker-nodes-body');
  const configBody = document.getElementById('docker-config-body');

  if (nodesBody) {
    loadDockerNodes();
  }
  if (configBody) {
    loadDockerConfig();
  }

  // --- Container Detail Page ---
  const containersBody = document.getElementById('containers-body');
  const imagesBody = document.getElementById('images-body');
  const showAllToggle = document.getElementById('show-all-toggle');
  const detailPanel = document.getElementById('container-detail-panel');
  const logTailInput = document.getElementById('docker-log-tail');
  const logTimestampsInput = document.getElementById('docker-log-timestamps');

  if (containersBody) {
    loadContainers(deviceId);
  }
  if (imagesBody) {
    loadImages(deviceId);
  }
  if (showAllToggle) {
    showAllToggle.addEventListener('change', function() {
      loadContainers(deviceId, showAllToggle.checked);
    });
  }

  // --- Functions ---

  async function loadDockerNodes() {
    try {
      const resp = await fetch('/api/docker/nodes');
      if (!resp.ok) throw new Error('Failed to load');
      const data = await resp.json();
      renderNodes(data.nodes || []);
    } catch (e) {
      if (nodesBody) nodesBody.innerHTML = '<tr><td colspan="5" class="muted">Failed to load Docker nodes: ' + e.message + '</td></tr>';
    }
  }

  function renderNodes(nodes) {
    if (!nodes.length) {
      if (nodesBody) nodesBody.innerHTML = '<tr><td colspan="5" class="muted">No Docker hosts configured.</td></tr>';
      return;
    }
    const html = nodes.map(function(n) {
      const statusClass = n.has_docker ? 'pill-ok' : 'pill-muted';
      const statusText = n.has_docker ? 'Docker available' : 'Docker not detected';
      const viewLink = n.has_docker
        ? '<a href="/docker/devices/' + n.device_id + '" class="button-secondary compact-button">View Containers</a>'
        : '';
      return '<tr>' +
        '<td>' + escapeHTML(n.device_name || n.device_id) + '</td>' +
        '<td>' + escapeHTML(n.hostname || '-') + '</td>' +
        '<td>' + escapeHTML(n.docker_host || '-') + '</td>' +
        '<td><span class="pill ' + statusClass + '">' + statusText + '</span></td>' +
        '<td>' + viewLink + '</td>' +
        '</tr>';
    }).join('');
    if (nodesBody) nodesBody.innerHTML = html;
  }

  async function loadDockerConfig() {
    try {
      const resp = await fetch(configAPI);
      if (!resp.ok) throw new Error('Failed to load');
      const data = await resp.json();
      renderConfig(data || []);
    } catch (e) {
      if (configBody) configBody.innerHTML = '<tr><td colspan="4" class="muted">Failed to load config: ' + e.message + '</td></tr>';
    }
  }

  function renderConfig(configs) {
    if (!configs.length) {
      if (configBody) configBody.innerHTML = '<tr><td colspan="4" class="muted">No Docker hosts configured.</td></tr>';
      return;
    }
    const html = configs.map(function(c) {
      const userDisplay = c.ssh_user || '<span class="muted">(default)</span>';
      const delLink = '<form method="post" action="/docker/config/' + c.device_id + '/delete" class="inline-form">' +
        '<button type="submit" class="button-secondary compact-button">Remove</button></form>';
      return '<tr>' +
        '<td><a class="device-link" href="/docker/devices/' + c.device_id + '">' + escapeHTML(c.device_name || c.device_id) + '</a></td>' +
        '<td>' + escapeHTML(c.docker_host || '-') + '</td>' +
        '<td>' + userDisplay + '</td>' +
        '<td>' + delLink + '</td>' +
        '</tr>';
    }).join('');
    if (configBody) configBody.innerHTML = html;
  }

  async function loadContainers(deviceId, showAll) {
    const url = '/api/docker/containers/' + encodeURIComponent(deviceId) + '?all=' + (showAll ? '1' : '0');
    try {
      const resp = await fetch(url);
      if (!resp.ok) throw new Error('Failed to load containers');
      const data = await resp.json();
      renderContainers(data.containers || []);
    } catch (e) {
      if (containersBody) containersBody.innerHTML = '<tr><td colspan="6" class="muted">Failed to load: ' + e.message + '</td></tr>';
    }
  }

  function renderContainers(containers) {
    if (!containers.length) {
      if (containersBody) containersBody.innerHTML = '<tr><td colspan="6" class="muted">No containers found.</td></tr>';
      return;
    }
    const html = containers.map(function(c) {
      const stateClass = c.state === 'running' ? 'pill-ok' : c.state === 'paused' ? 'pill-warn' : 'pill-muted';
      const name = escapeHTML(c.name);
      const encName = encodeURIComponent(c.name);
      const actionBtns = buildActionButtons(c.name, c.state);
      return '<tr>' +
        '<td><a href="#" class="container-detail-link" data-name="' + name + '">' + name + '</a></td>' +
        '<td><span class="subtle">' + escapeHTML(c.image) + '</span></td>' +
        '<td><span class="subtle">' + escapeHTML(c.status) + '</span></td>' +
        '<td><span class="pill ' + stateClass + '">' + escapeHTML(c.state) + '</span></td>' +
        '<td><span class="subtle">' + escapeHTML(c.ports || '-') + '</span></td>' +
        '<td>' + actionBtns + '</td>' +
        '</tr>';
    }).join('');
    if (containersBody) containersBody.innerHTML = html;

    // Attach action handlers
    if (containersBody) {
      containersBody.querySelectorAll('[data-action]').forEach(function(btn) {
        btn.addEventListener('click', handleContainerAction);
      });
      containersBody.querySelectorAll('[data-view]').forEach(function(btn) {
        btn.addEventListener('click', handleContainerView);
      });
      containersBody.querySelectorAll('.container-detail-link').forEach(function(link) {
        link.addEventListener('click', function(e) {
          e.preventDefault();
          showContainerDetail(deviceId, link.dataset.name);
        });
      });
    }
  }

  async function loadImages(deviceId) {
    try {
      const resp = await fetch('/api/docker/images/' + encodeURIComponent(deviceId));
      if (!resp.ok) throw new Error('Failed to load images');
      const data = await resp.json();
      renderImages(data.images || []);
    } catch (e) {
      if (imagesBody) imagesBody.innerHTML = '<tr><td colspan="5" class="muted">Failed to load: ' + e.message + '</td></tr>';
    }
  }

  function renderImages(images) {
    if (!images.length) {
      if (imagesBody) imagesBody.innerHTML = '<tr><td colspan="5" class="muted">No images found.</td></tr>';
      return;
    }
    const html = images.map(function(img) {
      const repo = img.repository === '<none>' ? '-' : escapeHTML(img.repository);
      const tag = img.tag === '<none>' ? '-' : escapeHTML(img.tag);
      return '<tr>' +
        '<td>' + repo + '</td>' +
        '<td><span class="subtle">' + tag + '</span></td>' +
        '<td><code class="subtle">' + escapeHTML(img.id.replace('sha256:', '').substring(0, 12)) + '</code></td>' +
        '<td><span class="subtle">' + formatBytes(img.size) + '</span></td>' +
        '<td><span class="subtle">' + escapeHTML(img.created_at || '-') + '</span></td>' +
        '</tr>';
    }).join('');
    if (imagesBody) imagesBody.innerHTML = html;
  }

  function buildActionButtons(name, state) {
    const encName = encodeURIComponent(name);
    const buttons = [];
    buttons.push('<button class="button-secondary compact-button" data-view="details" data-name="' + escapeHTML(name) + '">Details</button>');
    buttons.push('<button class="button-secondary compact-button" data-view="logs" data-name="' + escapeHTML(name) + '">Logs</button>');
    buttons.push('<button class="button-secondary compact-button" data-view="stats" data-name="' + escapeHTML(name) + '">Stats</button>');
    if (state !== 'running') {
      buttons.push('<button class="button-secondary compact-button" data-action="start" data-name="' + escapeHTML(name) + '">Start</button>');
    }
    if (state === 'running') {
      buttons.push('<button class="button-secondary compact-button" data-action="stop" data-name="' + escapeHTML(name) + '">Stop</button>');
      buttons.push('<button class="button-secondary compact-button" data-action="restart" data-name="' + escapeHTML(name) + '">Restart</button>');
      buttons.push('<button class="button-secondary compact-button" data-action="pause" data-name="' + escapeHTML(name) + '">Pause</button>');
    }
    if (state === 'paused') {
      buttons.push('<button class="button-secondary compact-button" data-action="unpause" data-name="' + escapeHTML(name) + '">Unpause</button>');
    }
    return buttons.join(' ');
  }

  async function handleContainerAction(e) {
    e.preventDefault();
    const btn = e.currentTarget;
    const action = btn.dataset.action;
    const name = btn.dataset.name;
    if (!confirm(action.charAt(0).toUpperCase() + action.slice(1) + ' container "' + name + '"?')) return;
    btn.disabled = true;
    try {
      const encName = encodeURIComponent(name);
      const resp = await fetch('/api/docker/containers/' + encodeURIComponent(deviceId) + '/' + encName + '/' + action, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      });
      if (!resp.ok) throw new Error('Action failed');
      // Refresh container list
      const showAll = document.getElementById('show-all-toggle') && document.getElementById('show-all-toggle').checked;
      loadContainers(deviceId, showAll);
    } catch (err) {
      alert('Action failed: ' + err.message);
      btn.disabled = false;
    }
  }

  function handleContainerView(e) {
    e.preventDefault();
    const view = e.currentTarget.dataset.view;
    const name = e.currentTarget.dataset.name;
    if (view === 'details') {
      showContainerDetail(deviceId, name);
    } else if (view === 'logs') {
      showContainerLogs(deviceId, name);
    } else if (view === 'stats') {
      showContainerStats(deviceId, name);
    }
  }

  async function showContainerDetail(deviceId, name) {
    const encName = encodeURIComponent(name);
    try {
      const resp = await fetch('/api/docker/containers/' + encodeURIComponent(deviceId) + '/' + encName + '/inspect');
      if (!resp.ok) throw new Error('Failed to load container details');
      const data = await resp.json();
      setDetailPanel('<div class="facts">' +
        '<p><span>Container</span><strong>' + escapeHTML(data.name) + '</strong></p>' +
        '<p><span>ID</span><code>' + escapeHTML(data.id ? data.id.substring(0, 12) : '-') + '</code></p>' +
        '<p><span>Image</span>' + escapeHTML(data.image || '-') + '</p>' +
        '<p><span>State</span>' + escapeHTML(data.state || '-') + '</p>' +
        '<p><span>Status</span>' + escapeHTML(data.status || '-') + '</p>' +
        '<p><span>Networks</span>' + escapeHTML(data.networks ? data.networks.join(', ') : '-') + '</p>' +
        '<p><span>Ports</span>' + escapeHTML(data.ports_detail ? data.ports_detail.map(function(p) {
          return (p.host_ip || '') + ':' + (p.host_port || '') + ' -> ' + p.cont_port + '/' + p.protocol;
        }).join(', ') : '-') + '</p>' +
        '<p><span>Command</span><code>' + escapeHTML(data.cmd ? data.cmd.join(' ') : '-') + '</code></p>' +
        '<p><span>Mounts</span>' + escapeHTML(data.mounts ? data.mounts.join(', ') : '-') + '</p>' +
        '</div>');
    } catch (e) {
      setDetailPanel('<p class="muted">Failed to load details: ' + escapeHTML(e.message) + '</p>');
    }
  }

  async function showContainerLogs(deviceId, name) {
    const encName = encodeURIComponent(name);
    try {
      const tail = logTailInput && logTailInput.value ? encodeURIComponent(logTailInput.value) : '200';
      const timestamps = logTimestampsInput && logTimestampsInput.checked ? 'true' : 'false';
      const resp = await fetch('/api/docker/containers/' + encodeURIComponent(deviceId) + '/' + encName + '/logs?tail=' + tail + '&timestamps=' + timestamps);
      if (!resp.ok) throw new Error('Failed to load logs');
      const rows = await resp.json();
      const html = '<h3>' + escapeHTML(name) + ' logs</h3><pre class="log-output">' + escapeHTML((rows || []).map(function(row) {
        return (row.timestamp && row.timestamp !== '0001-01-01T00:00:00Z' ? row.timestamp + ' ' : '') + row.message;
      }).join('\n') || 'No logs returned.') + '</pre>';
      setDetailPanel(html);
    } catch (e) {
      setDetailPanel('<p class="muted">Failed to load logs: ' + escapeHTML(e.message) + '</p>');
    }
  }

  async function showContainerStats(deviceId, name) {
    const encName = encodeURIComponent(name);
    try {
      const resp = await fetch('/api/docker/containers/' + encodeURIComponent(deviceId) + '/' + encName + '/stats');
      if (!resp.ok) throw new Error('Failed to load stats');
      const stats = await resp.json();
      setDetailPanel('<div class="facts">' +
        '<p><span>Container</span><strong>' + escapeHTML(name) + '</strong></p>' +
        '<p><span>CPU</span>' + escapeHTML((stats.cpu_percent || 0).toFixed ? stats.cpu_percent.toFixed(2) : stats.cpu_percent) + '%</p>' +
        '<p><span>Memory</span>' + formatBytes(stats.memory ? stats.memory.used : 0) + ' / ' + formatBytes(stats.memory ? stats.memory.limit : 0) + '</p>' +
        '<p><span>Memory %</span>' + escapeHTML(stats.memory ? stats.memory.percent : 0) + '%</p>' +
        '</div>');
    } catch (e) {
      setDetailPanel('<p class="muted">Failed to load stats: ' + escapeHTML(e.message) + '</p>');
    }
  }

  function setDetailPanel(html) {
    if (detailPanel) {
      detailPanel.className = '';
      detailPanel.innerHTML = html;
    }
  }

  // --- Utilities ---

  function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }
})();
