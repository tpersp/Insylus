package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"insylus/internal/httpx"
	"insylus/internal/pluginhost"
)

type runtime struct {
	store   store
	targets pluginhost.TargetService
	plugins pluginhost.PluginRegistry
	render  func(http.ResponseWriter, string, any)
}

var (
	monitorLoopOnce sync.Once
	monitorRunning  atomic.Bool
	pingLatencyRE   = regexp.MustCompile(`time[=<]([0-9.]+)`)
)

func newRuntime(host pluginhost.Host) runtime {
	return runtime{
		store:   newStore(host),
		targets: host.Targets(),
		plugins: host.Plugins(),
		render:  host.Web().Render,
	}
}

func (rt runtime) startLoop() {
	monitorLoopOnce.Do(func() {
		go func() {
			delay := 5 * time.Second
			for {
				time.Sleep(delay)
				if err := rt.runChecks(context.Background()); err != nil {
					log.Printf("monitor check failed: %v", err)
				}
				settings, err := rt.store.Settings(context.Background())
				if err != nil {
					log.Printf("monitor settings load failed: %v", err)
					delay = time.Duration(defaultIntervalSeconds) * time.Second
					continue
				}
				delay = time.Duration(settings.IntervalSeconds) * time.Second
			}
		}()
	})
}

func (rt runtime) handlePage(w http.ResponseWriter, r *http.Request) {
	statuses, settings, err := rt.pageData(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	upCount, downCount, unknownCount := summarize(statuses)
	rt.render(w, "monitor.html", map[string]any{
		"Statuses":     statuses,
		"Settings":     settings,
		"UpCount":      upCount,
		"DownCount":    downCount,
		"UnknownCount": unknownCount,
	})
}

func (rt runtime) pageData(ctx context.Context) ([]Status, Settings, error) {
	settings, err := rt.store.Settings(ctx)
	if err != nil {
		return nil, Settings{}, err
	}
	targets, err := rt.collectTargets(ctx)
	if err != nil {
		return nil, settings, err
	}
	statuses, err := rt.store.LatestStatuses(ctx, targets)
	if err != nil {
		return nil, settings, err
	}
	sortStatuses(statuses)
	return statuses, settings, nil
}

func (rt runtime) handleListAPI(w http.ResponseWriter, r *http.Request) {
	statuses, _, err := rt.pageData(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query != "" {
		filtered := statuses[:0]
		for _, status := range statuses {
			if strings.EqualFold(status.Key, query) ||
				strings.EqualFold(status.DeviceID, query) ||
				strings.EqualFold(status.Name, query) ||
				strings.EqualFold(status.Host, query) {
				filtered = append(filtered, status)
			}
		}
		statuses = filtered
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (rt runtime) handleHistoryAPI(w http.ResponseWriter, r *http.Request) {
	window := parseWindow(r.URL.Query().Get("window"))
	statuses, _, err := rt.pageData(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	status, ok := findStatusByKey(statuses, key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	points, err := rt.store.History(r.Context(), status.Key, window)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, HistoryResponse{
		Key:    status.Key,
		Window: window.String(),
		Points: points,
		Target: status,
	})
}

func (rt runtime) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	if err := rt.runChecks(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/monitor", http.StatusSeeOther)
}

func (rt runtime) handleCheckNowAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	if err := rt.runChecks(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (rt runtime) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	settings := normalizeSettings(parseInt(r.FormValue("interval_seconds")), parseInt(r.FormValue("timeout_millis")))
	if err := rt.store.SaveSettings(r.Context(), settings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/monitor", http.StatusSeeOther)
}

func (rt runtime) handleSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	var settings Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		httpx.InvalidRequest(w)
		return
	}
	settings = normalizeSettings(settings.IntervalSeconds, settings.TimeoutMillis)
	if err := rt.store.SaveSettings(r.Context(), settings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (rt runtime) handleManualTargetForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	target := ManualTarget{
		Name:    strings.TrimSpace(r.FormValue("name")),
		Host:    strings.TrimSpace(r.FormValue("host")),
		Port:    parseInt(r.FormValue("port")),
		Enabled: r.FormValue("enabled") != "false",
	}
	if target.Name == "" || target.Host == "" {
		http.Error(w, "name and host are required", http.StatusBadRequest)
		return
	}
	if err := rt.store.AddManualTarget(r.Context(), target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/monitor", http.StatusSeeOther)
}

func (rt runtime) handleManualTargetAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	var target ManualTarget
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		httpx.InvalidRequest(w)
		return
	}
	target.Name = strings.TrimSpace(target.Name)
	target.Host = strings.TrimSpace(target.Host)
	if target.Name == "" || target.Host == "" {
		http.Error(w, "name and host are required", http.StatusBadRequest)
		return
	}
	if err := rt.store.AddManualTarget(r.Context(), target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (rt runtime) handleManualTargetDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w, r)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid manual target ID", http.StatusBadRequest)
		return
	}
	if err := rt.store.DeleteManualTarget(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	http.Redirect(w, r, "/monitor", http.StatusSeeOther)
}

func (rt runtime) runChecks(ctx context.Context) error {
	if rt.plugins != nil && !rt.plugins.Enabled("monitor") {
		return nil
	}
	if !monitorRunning.CompareAndSwap(false, true) {
		return nil
	}
	defer monitorRunning.Store(false)

	settings, err := rt.store.Settings(ctx)
	if err != nil {
		return err
	}
	targets, err := rt.collectTargets(ctx)
	if err != nil {
		return err
	}
	timeout := time.Duration(settings.TimeoutMillis) * time.Millisecond
	samples := make([]sampleRecord, 0, len(targets))
	now := time.Now().UTC()
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		samples = append(samples, runCheck(target, timeout, now))
	}
	if err := rt.store.RecordSamples(ctx, samples); err != nil {
		return err
	}
	return nil
}

func (rt runtime) collectTargets(ctx context.Context) ([]monitorTarget, error) {
	list, err := rt.targets.List(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]monitorTarget, 0, len(list))
	for _, target := range list {
		host := resolveTargetHost(target)
		targets = append(targets, monitorTarget{
			Key:           target.ID,
			Source:        "device",
			DeviceID:      target.ID,
			Name:          target.Name,
			Host:          host,
			Enabled:       true,
			MonitorMethod: "ping",
		})
	}
	manualTargets, err := rt.store.ListManualTargets(ctx)
	if err != nil {
		return nil, err
	}
	for _, manual := range manualTargets {
		method := "ping"
		if manual.Port > 0 {
			method = "tcp"
		}
		targets = append(targets, monitorTarget{
			Key:            monitorKeyForManual(manual.ID),
			Source:         "manual",
			ManualTargetID: manual.ID,
			Name:           manual.Name,
			Host:           manual.Host,
			Port:           manual.Port,
			Enabled:        manual.Enabled,
			MonitorMethod:  method,
		})
	}
	sortMonitorTargets(targets)
	return targets, nil
}

func resolveTargetHost(target pluginhost.Target) string {
	if host := primaryIP(target.IPs); host != "" {
		return host
	}
	if host := parseHost(target.SSHHost); host != "" {
		return host
	}
	if host := parseHost(target.APIURL); host != "" {
		return host
	}
	return strings.TrimSpace(target.Hostname)
}

func primaryIP(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	sorted := append([]string(nil), ips...)
	sort.Slice(sorted, func(i, j int) bool {
		return compareHosts(sorted[i], sorted[j]) < 0
	})
	return strings.TrimSpace(sorted[0])
}

func parseHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil {
			return strings.TrimSpace(parsed.Hostname())
		}
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.TrimSpace(host)
	}
	return raw
}

func runCheck(target monitorTarget, timeout time.Duration, checkedAt time.Time) sampleRecord {
	record := sampleRecord{
		Key:       target.Key,
		Name:      target.Name,
		Source:    target.Source,
		DeviceID:  target.DeviceID,
		Host:      target.Host,
		Port:      target.Port,
		CheckedAt: checkedAt,
	}
	if target.Host == "" {
		record.Error = "no address configured"
		return record
	}
	if target.MonitorMethod == "tcp" && target.Port > 0 {
		success, latency, errText := tcpProbe(target.Host, target.Port, timeout)
		record.Success = success
		record.LatencyMs = latency
		record.Error = errText
		return record
	}
	success, latency, errText := pingProbe(target.Host, timeout)
	record.Success = success
	record.LatencyMs = latency
	record.Error = errText
	return record
}

func pingProbe(host string, timeout time.Duration) (bool, float64, string) {
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-n", "-c", "1", "-W", strconv.Itoa(seconds), host)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, 0, "ping timeout"
		}
		if text == "" {
			text = err.Error()
		}
		return false, 0, text
	}
	return true, parsePingLatency(text), ""
}

func parsePingLatency(output string) float64 {
	match := pingLatencyRE.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.ParseFloat(match[1], 64)
	return value
}

func tcpProbe(host string, port int, timeout time.Duration) (bool, float64, string) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false, 0, err.Error()
	}
	_ = conn.Close()
	return true, float64(time.Since(start).Microseconds()) / 1000, ""
}

func summarize(statuses []Status) (int, int, int) {
	upCount, downCount, unknownCount := 0, 0, 0
	for _, status := range statuses {
		switch status.State {
		case "up":
			upCount++
		case "down":
			downCount++
		default:
			unknownCount++
		}
	}
	return upCount, downCount, unknownCount
}

func findStatusByKey(statuses []Status, key string) (Status, bool) {
	for _, status := range statuses {
		if status.Key == key || status.DeviceID == key {
			return status, true
		}
	}
	return Status{}, false
}

func parseWindow(raw string) time.Duration {
	switch strings.TrimSpace(raw) {
	case "30m":
		return 30 * time.Minute
	case "24h":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

func parseInt(raw string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	return n
}

func sortMonitorTargets(targets []monitorTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Source != targets[j].Source {
			return targets[i].Source < targets[j].Source
		}
		if cmp := compareHosts(targets[i].Host, targets[j].Host); cmp != 0 {
			return cmp < 0
		}
		return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name)
	})
}

func sortStatuses(statuses []Status) {
	sort.SliceStable(statuses, func(i, j int) bool {
		if severity(statuses[i].State) != severity(statuses[j].State) {
			return severity(statuses[i].State) < severity(statuses[j].State)
		}
		if statuses[i].Source != statuses[j].Source {
			return statuses[i].Source < statuses[j].Source
		}
		if cmp := compareHosts(statuses[i].Host, statuses[j].Host); cmp != 0 {
			return cmp < 0
		}
		return strings.ToLower(statuses[i].Name) < strings.ToLower(statuses[j].Name)
	})
}

func severity(state string) int {
	switch state {
	case "down":
		return 0
	case "paused":
		return 1
	case "unknown":
		return 2
	case "up":
		return 3
	default:
		return 4
	}
}

func compareHosts(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if ap, err := netip.ParseAddr(a); err == nil {
		if bp, err := netip.ParseAddr(b); err == nil {
			return ap.Compare(bp)
		}
		return -1
	}
	if _, err := netip.ParseAddr(b); err == nil {
		return 1
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}
