package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"insylus/internal/shared"
	"insylus/internal/version"
)

const Version = version.AgentVersion

type Config struct {
	ServerURL      string        `json:"server_url"`
	DeviceID       string        `json:"device_id"`
	AgentToken     string        `json:"agent_token"`
	BootstrapToken string        `json:"bootstrap_token,omitempty"`
	Interval       time.Duration `json:"interval"`
}

type Runner struct {
	client *http.Client
	cfg    Config
}

func New(cfg Config) *Runner {
	if cfg.Interval == 0 {
		cfg.Interval = shared.AgentCheckInInterval
	}
	if cfg.Interval > shared.AgentCheckInInterval {
		cfg.Interval = shared.AgentCheckInInterval
	}
	return &Runner{
		client: &http.Client{Timeout: 20 * time.Second},
		cfg:    cfg,
	}
}

func (r *Runner) Bootstrap(ctx context.Context, bootstrapToken string) (Config, error) {
	hostname, _ := os.Hostname()
	health := collectHealth()
	reqBody := shared.BootstrapRequest{
		BootstrapToken: bootstrapToken,
		Hostname:       hostname,
		OSName:         health.OSName,
		AgentVersion:   Version,
	}
	var resp shared.BootstrapResponse
	if err := r.doJSON(ctx, http.MethodPost, "/api/bootstrap/register", "", reqBody, &resp); err != nil {
		return Config{}, err
	}
	interval, _ := time.ParseDuration(resp.Interval)
	if interval == 0 {
		interval = shared.AgentCheckInInterval
	}
	return Config{
		ServerURL:      r.cfg.ServerURL,
		DeviceID:       resp.DeviceID,
		AgentToken:     resp.AgentToken,
		BootstrapToken: bootstrapToken,
		Interval:       interval,
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		if err := r.syncOnce(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "sync failed: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) syncOnce(ctx context.Context) error {
	health := collectHealth()
	if err := r.doJSON(ctx, http.MethodPost, "/api/checkin", r.cfg.AgentToken, shared.CheckInRequest{Health: health}, nil); err != nil {
		return err
	}
	var policy shared.AgentPolicyResponse
	policyPath := fmt.Sprintf("/api/policy?goos=%s&goarch=%s", runtime.GOOS, runtime.GOARCH)
	if err := r.doJSON(ctx, http.MethodGet, policyPath, r.cfg.AgentToken, nil, &policy); err != nil {
		return err
	}
	if didUpdate, err := r.maybeAutoUpdate(ctx, policy.AgentUpdate); didUpdate || err != nil {
		return err
	}
	report := applyPolicy(policy)
	report.LastPolicyHealth = health
	report.DeviceID = policy.DeviceID
	report.Topology = collectTopologyDiscovery()
	if err := r.doJSON(ctx, http.MethodPost, "/api/report", r.cfg.AgentToken, report, nil); err != nil {
		return err
	}
	return nil
}

func (r *Runner) doJSON(ctx context.Context, method, path, token string, requestBody any, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(requestBody); err != nil {
			return err
		}
		body = buf
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.cfg.ServerURL, "/")+path, body)
	if err != nil {
		return err
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if responseBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(responseBody)
}

func Install(currentBinary, serverURL, bootstrapToken string) error {
	paths := installPathsFromEnv()
	logStep("Registering host with Insylus", "server=%s", serverURL)
	runner := New(Config{ServerURL: serverURL})
	cfg, err := runner.Bootstrap(context.Background(), bootstrapToken)
	if err != nil {
		return err
	}
	logStep("Registration complete", "device_id=%s interval=%s", cfg.DeviceID, cfg.Interval)

	logStep("Preparing configuration directory", "path=%s", filepath.Dir(paths.ConfigPath))
	if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o755); err != nil {
		return err
	}
	serviceExists, err := systemdUnitExists(paths.ServiceName)
	if err != nil {
		return err
	}
	serviceWasActive := false
	if serviceExists {
		serviceWasActive, err = systemdUnitActive(paths.ServiceName)
		if err != nil {
			return err
		}
		if serviceWasActive {
			logStep("Stopping existing agent service", "unit=%s", paths.ServiceName)
			if err := runSystemctlWithRetry("stop", paths.ServiceName); err != nil {
				return err
			}
		}
	}
	samePath, err := sameFilePath(currentBinary, paths.BinaryPath)
	if err != nil {
		return err
	}
	if !samePath {
		logStep("Installing agent binary", "src=%s dst=%s", currentBinary, paths.BinaryPath)
		if err := copyFile(currentBinary, paths.BinaryPath, 0o755); err != nil {
			return err
		}
	} else {
		logStep("Agent binary already in final location", "path=%s", paths.BinaryPath)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	logStep("Writing agent config", "path=%s", paths.ConfigPath)
	if err := os.WriteFile(paths.ConfigPath, data, 0o600); err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=Insylus Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=INSYLUS_AGENT_BIN_PATH=%s
Environment=INSYLUS_AGENT_CONFIG_PATH=%s
Environment=INSYLUS_AGENT_SERVICE_NAME=%s
Environment=INSYLUS_AGENT_UNIT_PATH=%s
ExecStart=%s run --config %s
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
`, paths.BinaryPath, paths.ConfigPath, paths.ServiceName, paths.UnitPath, paths.BinaryPath, paths.ConfigPath)
	logStep("Writing systemd unit", "path=%s", paths.UnitPath)
	if err := os.WriteFile(paths.UnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	for _, cmd := range [][]string{
		{"daemon-reload"},
		{"enable", paths.ServiceName},
		{"restart", paths.ServiceName},
	} {
		logStep("Running command", "systemctl %s", strings.Join(cmd, " "))
		if err := runSystemctlWithRetry(cmd...); err != nil {
			return err
		}
	}
	state, err := runSystemctlOutput("is-active", paths.ServiceName)
	if err != nil {
		return err
	}
	fmt.Printf("\nInsylus agent installation successful.\n")
	fmt.Printf("Installed binary: %s\n", paths.BinaryPath)
	fmt.Printf("Config file: %s\n", paths.ConfigPath)
	fmt.Printf("Service unit: %s\n", paths.UnitPath)
	fmt.Printf("Registered device ID: %s\n", cfg.DeviceID)
	fmt.Printf("Check-in interval: %s\n", cfg.Interval)
	fmt.Printf("Service state: %s\n", strings.TrimSpace(state))
	fmt.Printf("Useful commands:\n")
	fmt.Printf("  systemctl status --no-pager %s\n", paths.ServiceName)
	fmt.Printf("  journalctl -u %s -f\n", paths.ServiceName)
	return nil
}

type installPaths struct {
	BinaryPath  string
	ConfigPath  string
	ServiceName string
	UnitPath    string
}

func installPathsFromEnv() installPaths {
	serviceName := firstEnv("INSYLUS_AGENT_SERVICE_NAME", "insylus-agent.service")
	return installPaths{
		BinaryPath:  firstEnv("INSYLUS_AGENT_BIN_PATH", "/usr/local/bin/insylus-agent"),
		ConfigPath:  firstEnv("INSYLUS_AGENT_CONFIG_PATH", "/etc/insylus-agent/config.json"),
		ServiceName: serviceName,
		UnitPath:    firstEnv("INSYLUS_AGENT_UNIT_PATH", filepath.Join("/etc/systemd/system", serviceName)),
	}
}

func firstEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (r *Runner) maybeAutoUpdate(ctx context.Context, manifest shared.AgentUpdateManifest) (bool, error) {
	if !manifest.Enabled {
		return false, nil
	}
	if manifest.Status == shared.AgentUpdateStatusUnsupported {
		_ = r.reportAgentUpdate(ctx, shared.AgentUpdateReport{
			Status:             shared.AgentUpdateStatusUnsupported,
			Error:              manifest.Error,
			ServerAgentVersion: manifest.ServerAgentVersion,
		})
		return false, nil
	}
	if compareVersions(Version, manifest.ServerAgentVersion) >= 0 {
		return false, nil
	}
	if manifest.DownloadURL == "" || manifest.SHA256 == "" {
		_ = r.reportAgentUpdate(ctx, shared.AgentUpdateReport{
			Status:             shared.AgentUpdateStatusFailed,
			Error:              "update manifest missing download URL or checksum",
			ServerAgentVersion: manifest.ServerAgentVersion,
			AttemptedAt:        time.Now().UTC(),
		})
		return false, nil
	}
	_ = r.reportAgentUpdate(ctx, shared.AgentUpdateReport{
		Status:             shared.AgentUpdateStatusUpdating,
		ServerAgentVersion: manifest.ServerAgentVersion,
		AttemptedAt:        time.Now().UTC(),
	})
	if err := installAgentUpdate(ctx, manifest); err != nil {
		_ = r.reportAgentUpdate(ctx, shared.AgentUpdateReport{
			Status:             shared.AgentUpdateStatusFailed,
			Error:              err.Error(),
			ServerAgentVersion: manifest.ServerAgentVersion,
			AttemptedAt:        time.Now().UTC(),
		})
		return false, nil
	}
	_ = r.reportAgentUpdate(ctx, shared.AgentUpdateReport{
		Status:             shared.AgentUpdateStatusUpdated,
		ServerAgentVersion: manifest.ServerAgentVersion,
		AttemptedAt:        time.Now().UTC(),
	})
	go sleepBeforeExit()
	return true, nil
}

func (r *Runner) reportAgentUpdate(ctx context.Context, report shared.AgentUpdateReport) error {
	return r.doJSON(ctx, http.MethodPost, "/api/agent/update-status", r.cfg.AgentToken, report, nil)
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.ServerURL == "" || cfg.AgentToken == "" {
		return Config{}, errors.New("invalid config")
	}
	if cfg.Interval == 0 {
		cfg.Interval = shared.AgentCheckInInterval
	}
	if cfg.Interval > shared.AgentCheckInInterval {
		cfg.Interval = shared.AgentCheckInInterval
	}
	return cfg, nil
}

func collectHealth() shared.HealthSnapshot {
	hostname, _ := os.Hostname()
	return shared.HealthSnapshot{
		Hostname:     hostname,
		OSName:       detectOSName(),
		IPs:          detectIPs(),
		Uptime:       readUptime(),
		LoadAverage:  readLoad(),
		MemoryUsed:   readMemory(),
		DiskUsed:     readDisk(),
		AgentVersion: Version,
		AgentGOOS:    runtime.GOOS,
		AgentGOARCH:  runtime.GOARCH,
	}
}

func detectOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(line[len("PRETTY_NAME="):], `"`)
		}
	}
	return "linux"
}

func detectIPs() []string {
	ips, _ := lookupIPv4("")
	return ips
}

func lookupIPv4(hostname string) ([]string, error) {
	if hostname != "" {
		addrs, err := net.LookupIP(hostname)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, addr := range addrs {
			if v4 := addr.To4(); v4 != nil {
				out = append(out, v4.String())
			}
		}
		return out, nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []string
	var fallback []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip != nil && ip.To4() != nil {
				ipStr := ip.String()
				fallback = appendIfMissing(fallback, ipStr)
				if isPreferredHostInterface(iface.Name) {
					out = appendIfMissing(out, ipStr)
				}
			}
		}
	}
	if len(out) == 0 {
		return fallback, nil
	}
	return out, nil
}

func appendIfMissing(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func isPreferredHostInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{
		"docker",
		"br-",
		"veth",
		"cni",
		"flannel",
		"virbr",
		"zt",
	} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}

func readUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return "unknown"
	}
	return parts[0] + "s"
}

func readLoad() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return "unknown"
	}
	return strings.Join(parts[:3], " ")
}

func readMemory() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unknown"
	}
	var total, available int64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d kB", &total)
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d kB", &available)
		}
	}
	if total == 0 {
		return "unknown"
	}
	used := total - available
	return fmt.Sprintf("%.1f%%", float64(used)*100/float64(total))
}

func readDisk() string {
	var stat syscallStatfs
	if err := statfs("/", &stat); err != nil {
		return "unknown"
	}
	if stat.Blocks == 0 {
		return "unknown"
	}
	used := stat.Blocks - stat.Bavail
	return fmt.Sprintf("%.1f%%", float64(used)*100/float64(stat.Blocks))
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	return nil
}

func sameFilePath(a, b string) (bool, error) {
	aPath, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	bPath, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return filepath.Clean(aPath) == filepath.Clean(bPath), nil
}

func logStep(step, format string, args ...any) {
	if format == "" {
		fmt.Printf("==> %s\n", step)
		return
	}
	fmt.Printf("==> %s: %s\n", step, fmt.Sprintf(format, args...))
}

func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func runCommand(name string, args ...string) error {
	_, err := runCommandOutput(name, args...)
	return err
}

func runSystemctlWithRetry(args ...string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := runSystemctlOutput(args...); err != nil {
			lastErr = err
			if attempt < 3 {
				logStep("Retrying systemctl command", "attempt=%d command=systemctl %s error=%v", attempt+1, strings.Join(args, " "), err)
				time.Sleep(time.Duration(attempt*2) * time.Second)
				continue
			}
			break
		}
		return nil
	}
	return lastErr
}

func runSystemctlOutput(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	cmd.Env = append(os.Environ(), "SYSTEMD_BUS_TIMEOUT=120s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("systemctl: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func systemdUnitExists(unit string) (bool, error) {
	_, err := runSystemctlOutput("status", unit)
	if err == nil {
		return true, nil
	}
	if systemdStatusExitCodeMeansExists(err) {
		return true, nil
	}
	if systemdStatusExitCodeMeansMissing(err) {
		return false, nil
	}
	return false, err
}

func systemdStatusExitCodeMeansExists(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	switch exitErr.ExitCode() {
	case 1, 3:
		return true
	default:
		return false
	}
}

func systemdStatusExitCodeMeansMissing(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 4
}

func systemdUnitActive(unit string) (bool, error) {
	_, err := runSystemctlOutput("is-active", "--quiet", unit)
	if err == nil {
		return true, nil
	}
	if systemdActiveExitCodeMeansInactive(err) {
		return false, nil
	}
	return false, err
}

func systemdActiveExitCodeMeansInactive(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	switch exitErr.ExitCode() {
	case 1, 3, 4:
		return true
	default:
		return false
	}
}
