(function() {
  'use strict';

  const api = window.UPDATE_API || {};
  const currentVersion = window.CURRENT_VERSION || '0.0.0';

  const checkBtn = document.getElementById('check-updates-btn');
  const latestVersionEl = document.getElementById('latest-version');
  const releaseNotesEl = document.getElementById('release-notes');
  const updateStatusEl = document.getElementById('update-status');
  const autoUpdateToggle = document.getElementById('auto-update-toggle');

  let latestInfo = null;

  if (checkBtn) {
    checkBtn.addEventListener('click', checkForUpdates);
  }

  if (autoUpdateToggle) {
    autoUpdateToggle.addEventListener('change', function() {
      toggleAutoUpdate(autoUpdateToggle.checked);
    });
  }

  async function checkForUpdates() {
    if (checkBtn) {
      checkBtn.disabled = true;
      checkBtn.textContent = 'Checking...';
    }
    updateStatusEl.innerHTML = '';

    try {
      const resp = await fetch(api.check, { method: 'POST' });
      if (!resp.ok) throw new Error('Failed to check for updates');
      latestInfo = await resp.json();

      if (latestVersionEl) {
        latestVersionEl.textContent = latestInfo.latest_version || 'Unknown';
      }

      if (latestInfo.release_notes) {
        releaseNotesEl.innerHTML = '<div class="markdown-body">' + escapeHTML(latestInfo.release_notes).replace(/\n/g, '<br>') + '</div>';
      }

      if (latestInfo.update_available) {
        showUpdateAvailable(latestInfo);
      } else {
        updateStatusEl.innerHTML = '<div class="alert alert-ok">You are running the latest version (' + currentVersion + ').</div>';
      }
    } catch (e) {
      updateStatusEl.innerHTML = '<div class="alert alert-error">Failed to check for updates: ' + escapeHTML(e.message) + '</div>';
      if (latestVersionEl) {
        latestVersionEl.textContent = 'Error';
      }
    } finally {
      if (checkBtn) {
        checkBtn.disabled = false;
        checkBtn.textContent = 'Check for Updates';
      }
    }
  }

  function showUpdateAvailable(info) {
    const skipBtn = '<button type="button" class="button-secondary" onclick="skipVersion(\'' + info.latest_version + '\')">Skip This Version</button>';
    updateStatusEl.innerHTML = `
      <div class="alert alert-warn">
        <strong>Update Available</strong><br>
        A new version (${escapeHTML(info.latest_version)}) is available.<br>
        The server will restart during the update process.
      </div>
      <div class="button-group">
        <button type="button" class="button-primary" onclick="applyUpdate('${escapeHTML(info.latest_version)}')">Update Now</button>
        ${skipBtn}
      </div>
    `;
  }

  window.applyUpdate = async function(version) {
    if (!confirm('Are you sure you want to update to version ' + version + '? The server will restart.')) {
      return;
    }

    const btn = document.querySelector('.button-primary');
    if (btn) {
      btn.disabled = true;
      btn.textContent = 'Updating...';
    }

    try {
      const resp = await fetch(api.apply, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: version })
      });
      if (!resp.ok) throw new Error('Failed to start update');
      const result = await resp.json();

      updateStatusEl.innerHTML = '<div class="alert alert-info">' + escapeHTML(result.message) + ' This page will refresh automatically.</div>';

      // Poll for status changes and refresh
      setTimeout(function() {
        location.reload();
      }, 5000);
    } catch (e) {
      updateStatusEl.innerHTML = '<div class="alert alert-error">Update failed: ' + escapeHTML(e.message) + '</div>';
      if (btn) {
        btn.disabled = false;
        btn.textContent = 'Update Now';
      }
    }
  };

  window.skipVersion = async function(version) {
    try {
      const resp = await fetch(api.skip, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: version })
      });
      if (!resp.ok) throw new Error('Failed to skip version');
      checkForUpdates();
    } catch (e) {
      alert('Failed to skip version: ' + e.message);
    }
  };

  window.rollbackUpdate = async function(id) {
    if (!confirm('Are you sure you want to rollback? The server will restart.')) {
      return;
    }

    try {
      const resp = await fetch(api.rollback + '/' + id, { method: 'POST' });
      if (!resp.ok) throw new Error('Failed to rollback');
      location.reload();
    } catch (e) {
      alert('Rollback failed: ' + e.message);
    }
  };

  async function toggleAutoUpdate(enabled) {
    try {
      const resp = await fetch(api.autoUpdate, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: enabled })
      });
      if (!resp.ok) throw new Error('Failed to update setting');
    } catch (e) {
      alert('Failed to update auto-update setting: ' + e.message);
      autoUpdateToggle.checked = !enabled;
    }
  }

  function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
})();
