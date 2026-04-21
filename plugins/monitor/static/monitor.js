(function () {
  function renderSparkline(el, points) {
    if (!points || !points.length) {
      el.textContent = 'No recent samples';
      return;
    }
    const width = 140;
    const height = 34;
    const padding = 3;
    const successes = points.filter((point) => point.success && point.latency_ms > 0);
    const minLatency = successes.length ? Math.min(...successes.map((point) => point.latency_ms)) : 0;
    const maxLatency = successes.length ? Math.max(...successes.map((point) => point.latency_ms)) : 1;
    const range = Math.max(maxLatency - minLatency, 1);
    const step = points.length > 1 ? (width - padding * 2) / (points.length - 1) : 0;

    let path = '';
    const failureDots = [];
    points.forEach((point, index) => {
      const x = padding + step * index;
      const y = point.success
        ? height - padding - ((point.latency_ms - minLatency) / range) * (height - padding * 2)
        : height - padding;
      if (point.success) {
        path += `${path ? ' L ' : 'M '}${x.toFixed(2)} ${y.toFixed(2)}`;
      } else {
        failureDots.push(`<circle cx="${x.toFixed(2)}" cy="${y.toFixed(2)}" r="2.2" class="monitor-sparkline-fail" />`);
      }
    });

    el.innerHTML = `
      <svg viewBox="0 0 ${width} ${height}" aria-hidden="true">
        <line x1="0" y1="${height - 1}" x2="${width}" y2="${height - 1}" class="monitor-sparkline-base"></line>
        ${path ? `<path d="${path}" class="monitor-sparkline-line"></path>` : ''}
        ${failureDots.join('')}
      </svg>
    `;
  }

  async function loadSparkline(el) {
    const key = el.dataset.key;
    const windowValue = el.dataset.window || '1h';
    try {
      const response = await fetch(`${window.MONITOR_API.historyBase}${encodeURIComponent(key)}/history?window=${encodeURIComponent(windowValue)}`);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const payload = await response.json();
      renderSparkline(el, payload.points || []);
    } catch (error) {
      el.textContent = 'Trend unavailable';
    }
  }

  document.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('.monitor-sparkline[data-key]').forEach(loadSparkline);
  });
}());
