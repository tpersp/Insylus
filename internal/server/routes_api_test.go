package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
	"insylus/plugins/registry"
)

func TestPluginRuntimeAPI(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	var plugins []pluginhost.PluginManifest
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/plugins", "", nil, &plugins)
	if len(plugins) == 0 {
		t.Fatalf("expected available plugins")
	}

	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/plugins/access/enable", "", nil, nil)
	var after []pluginhost.PluginManifest
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/plugins", "", nil, &after)
	if !pluginEnabled(after, "access") {
		t.Fatalf("expected access enabled after API call: %+v", after)
	}

	var profiles []pluginhost.PluginProfile
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/plugins/profiles", "", nil, &profiles)
	if len(profiles) == 0 {
		t.Fatalf("expected plugin profiles")
	}
	for _, profile := range profiles {
		if !containsString(profile.PluginIDs, "dashboard") {
			t.Fatalf("profile %q does not include dashboard: %+v", profile.Name, profile.PluginIDs)
		}
	}
}

func TestPluginDisableGatesRegisteredSurfacesWithoutRestart(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()
	handler := app.Handler()

	if status := responseStatus(handler, http.MethodGet, "/update", nil); status != http.StatusOK {
		t.Fatalf("GET /update before disable status = %d, want 200", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/plugin-assets/update/update.js", nil); status != http.StatusOK {
		t.Fatalf("GET update asset before disable status = %d, want 200", status)
	}

	doJSONRequest(t, handler, http.MethodPost, "/api/plugins/update/disable", "", nil, nil)

	if status := responseStatus(handler, http.MethodGet, "/update", nil); status != http.StatusNotFound {
		t.Fatalf("GET /update after disable status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodPost, "/api/update/check", nil); status != http.StatusNotFound {
		t.Fatalf("POST /api/update/check after disable status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/plugin-assets/update/update.js", nil); status != http.StatusNotFound {
		t.Fatalf("GET update asset after disable status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/api/plugins", nil); status != http.StatusOK {
		t.Fatalf("GET /api/plugins after update disable status = %d, want 200", status)
	}
}

func TestAccessDisableGatesSettingsRoutesWithoutRestart(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "access")
	defer app.Close()
	handler := app.Handler()

	if status := responseStatus(handler, http.MethodGet, "/access/settings", nil); status != http.StatusOK {
		t.Fatalf("GET /access/settings before disable status = %d, want 200", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/settings", nil); status != http.StatusOK {
		t.Fatalf("GET /settings before disable status = %d, want 200", status)
	}

	doJSONRequest(t, handler, http.MethodPost, "/api/plugins/access/disable", "", nil, nil)

	if status := responseStatus(handler, http.MethodGet, "/access/settings", nil); status != http.StatusNotFound {
		t.Fatalf("GET /access/settings after disable status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/settings", nil); status != http.StatusNotFound {
		t.Fatalf("GET /settings after disable status = %d, want 404", status)
	}

	form := url.Values{"managed_user": {"remote"}}
	if status := responseStatus(handler, http.MethodPost, "/access/settings/managed-account", bytes.NewBufferString(form.Encode())); status != http.StatusNotFound {
		t.Fatalf("POST /access/settings/managed-account after disable status = %d, want 404", status)
	}
}

func TestDisabledAtStartupRoutesDoNotFallThroughToDashboard(t *testing.T) {
	app := newTestAppWithOnlyPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "dashboard", "help")
	defer app.Close()
	handler := app.Handler()

	if status := responseStatus(handler, http.MethodGet, "/", nil); status != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/devices", nil); status != http.StatusNotFound {
		t.Fatalf("GET /devices with devices disabled at startup status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/api/devices", nil); status != http.StatusNotFound {
		t.Fatalf("GET /api/devices with devices disabled at startup status = %d, want 404", status)
	}
}

func TestPluginEnableActivatesRoutesWithoutRestart(t *testing.T) {
	app := newTestAppWithOnlyPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "dashboard", "help")
	defer app.Close()
	handler := app.Handler()

	if status := responseStatus(handler, http.MethodGet, "/discovery", nil); status != http.StatusNotFound {
		t.Fatalf("GET /discovery before enable status = %d, want 404", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/api/discovery", nil); status != http.StatusNotFound {
		t.Fatalf("GET /api/discovery before enable status = %d, want 404", status)
	}

	var enableResp map[string]string
	doJSONRequest(t, handler, http.MethodPost, "/api/plugins/discovery/enable", "", nil, &enableResp)
	if enableResp["restart"] != "not_required" {
		t.Fatalf("enable response restart = %q, want not_required", enableResp["restart"])
	}

	if status := responseStatus(handler, http.MethodGet, "/discovery", nil); status != http.StatusOK {
		t.Fatalf("GET /discovery after enable status = %d, want 200", status)
	}
	if status := responseStatus(handler, http.MethodGet, "/api/discovery", nil); status != http.StatusOK {
		t.Fatalf("GET /api/discovery after enable status = %d, want 200", status)
	}
}

func TestWebLandingAndDevicesRoutes(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	root := httptest.NewRecorder()
	app.Handler().ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", root.Code)
	}
	if !bytes.Contains(root.Body.Bytes(), []byte("Access Scope")) {
		t.Fatalf("GET / did not render dashboard: %s", root.Body.String())
	}

	devices := httptest.NewRecorder()
	app.Handler().ServeHTTP(devices, httptest.NewRequest(http.MethodGet, "/devices", nil))
	if devices.Code != http.StatusOK {
		t.Fatalf("GET /devices status = %d, want 200", devices.Code)
	}
	if !bytes.Contains(devices.Body.Bytes(), []byte("Devices")) {
		t.Fatalf("GET /devices did not render devices page: %s", devices.Body.String())
	}
}

func TestTargetsAPI(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	req := pluginhost.TargetInput{
		Name:     "docker01",
		Kind:     "docker-host",
		Hostname: "docker01.local",
		IPs:      []string{"10.0.0.10"},
		APIURL:   "tcp://docker01.local:2376",
	}
	var target pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/targets", "", req, &target)
	if target.ID == "" || target.Name != "docker01" || target.Kind != "docker-host" {
		t.Fatalf("unexpected target: %+v", target)
	}

	var targets []pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/targets/find?q=docker01.local", "", nil, &targets)
	if len(targets) != 1 || targets[0].ID != target.ID {
		t.Fatalf("unexpected find result: %+v", targets)
	}
}

func TestDockerConfigCreatesStandaloneTarget(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "docker")
	defer app.Close()

	type dockerConfigSummary struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		Hostname   string `json:"hostname"`
		SSHUser    string `json:"ssh_user"`
		DockerHost string `json:"docker_host"`
	}

	var created dockerConfigSummary
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/docker/config", "", map[string]string{
		"device_name": "docker01",
		"docker_host": "10.0.0.20",
		"ssh_user":    "operator",
	}, &created)
	if created.DeviceID == "" || created.DeviceName != "docker01" || created.DockerHost != "10.0.0.20" || created.SSHUser != "operator" {
		t.Fatalf("unexpected Docker config: %+v", created)
	}

	var target pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/targets/"+created.DeviceID, "", nil, &target)
	if target.Kind != "docker-host" || target.Hostname != "10.0.0.20" || target.SSHHost != "10.0.0.20" || target.SSHUser != "operator" {
		t.Fatalf("unexpected Docker target: %+v", target)
	}

	var configs []dockerConfigSummary
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/docker/config", "", nil, &configs)
	if len(configs) != 1 || configs[0].DeviceID != created.DeviceID {
		t.Fatalf("unexpected Docker config list: %+v", configs)
	}
}

func TestDockerConfigPrefersPluginConnectionAndDeleteKeepsTarget(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "docker")
	defer app.Close()

	var target pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/targets", "", pluginhost.TargetInput{
		Name:     "legacy-docker",
		Kind:     "linux-host",
		Hostname: "inventory-host.local",
		SSHHost:  "inventory-ssh.local",
		SSHUser:  "inventory-user",
	}, &target)

	type dockerConfigSummary struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		Hostname   string `json:"hostname"`
		SSHUser    string `json:"ssh_user"`
		DockerHost string `json:"docker_host"`
	}
	var created dockerConfigSummary
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/docker/config", "", map[string]string{
		"device_id":   target.ID,
		"docker_host": "plugin-docker.local",
		"ssh_user":    "plugin-user",
	}, &created)
	if created.DockerHost != "plugin-docker.local" || created.SSHUser != "plugin-user" {
		t.Fatalf("expected plugin connection details to win, got %+v", created)
	}

	var fetched dockerConfigSummary
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/docker/config/"+target.ID, "", nil, &fetched)
	if fetched.DeviceName != "legacy-docker" || fetched.DockerHost != "plugin-docker.local" || fetched.SSHUser != "plugin-user" {
		t.Fatalf("unexpected fetched config: %+v", fetched)
	}

	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/docker/config/"+target.ID+"/delete", "", nil, nil)
	var stillThere pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/targets/"+target.ID, "", nil, &stillThere)
	if stillThere.ID != target.ID {
		t.Fatalf("expected target to remain after Docker config delete, got %+v", stillThere)
	}
}

func TestDiscoveryPluginListsAndPromotesCandidates(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "discovery")
	defer app.Close()

	now := "2026-04-21T12:00:00Z"
	res, err := app.store.db.ExecContext(context.Background(), `
		insert into discovered_devices (
			fingerprint, display_name, hostname, ip_address, mac_address, open_ports_json,
			status, status_note, source_cidr, kind_hint, first_seen_at, last_seen_at, created_at, updated_at
		)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ip:10.10.10.20",
		"atlas",
		"atlas.local",
		"10.10.10.20",
		"aa:bb:cc:dd:ee:ff",
		`[22,443]`,
		"pending",
		"",
		"10.10.10.0/24",
		"linux-host",
		now,
		now,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert discovered device: %v", err)
	}
	candidateID, _ := res.LastInsertId()

	type discoveredCandidate struct {
		ID               int64  `json:"id"`
		DisplayName      string `json:"display_name"`
		IPAddress        string `json:"ip_address"`
		Status           string `json:"status"`
		PromotedTargetID string `json:"promoted_target_id"`
	}
	var candidates []discoveredCandidate
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/discovery", "", nil, &candidates)
	if len(candidates) != 1 || candidates[0].DisplayName != "atlas" || candidates[0].IPAddress != "10.10.10.20" {
		t.Fatalf("unexpected discovery list: %+v", candidates)
	}

	type promoteResponse struct {
		Candidate discoveredCandidate `json:"candidate"`
		TargetID  string              `json:"target_id"`
	}
	var promoted promoteResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/discovery/"+strconv.FormatInt(candidateID, 10)+"/promote", "", nil, &promoted)
	if promoted.TargetID == "" || promoted.Candidate.Status != "promoted" {
		t.Fatalf("unexpected discovery promote response: %+v", promoted)
	}

	var target pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/targets/"+promoted.TargetID, "", nil, &target)
	if target.Name != "atlas" || target.Hostname != "atlas.local" || len(target.IPs) != 1 || target.IPs[0] != "10.10.10.20" {
		t.Fatalf("unexpected promoted target: %+v", target)
	}
}

func TestDiscoveryPluginSortsByIPAndMarksKnownDevices(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "discovery")
	defer app.Close()

	var knownTarget pluginhost.Target
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/targets", "", pluginhost.TargetInput{
		Name:     "MiscServer",
		Kind:     "linux-host",
		Hostname: "miscserver.local",
		IPs:      []string{"10.10.10.22"},
	}, &knownTarget)

	now := "2026-04-21T12:00:00Z"
	for _, row := range []struct {
		fingerprint string
		name        string
		host        string
		ip          string
	}{
		{"ip:10.10.10.100", "device-100", "", "10.10.10.100"},
		{"ip:10.10.10.22", "MiscServer", "miscserver.local", "10.10.10.22"},
		{"ip:10.10.10.3", "device-3", "", "10.10.10.3"},
	} {
		if _, err := app.store.db.ExecContext(context.Background(), `
			insert into discovered_devices (
				fingerprint, display_name, hostname, ip_address, mac_address, open_ports_json,
				status, status_note, source_cidr, kind_hint, first_seen_at, last_seen_at, created_at, updated_at
			)
			values (?, ?, ?, ?, '', '[]', 'pending', '', '10.10.10.0/24', 'linux-host', ?, ?, ?, ?)`,
			row.fingerprint, row.name, row.host, row.ip, now, now, now, now); err != nil {
			t.Fatalf("insert discovered device %s: %v", row.ip, err)
		}
	}

	type discoveredCandidate struct {
		DisplayName   string `json:"display_name"`
		Hostname      string `json:"hostname"`
		ID            int64  `json:"id"`
		IPAddress     string `json:"ip_address"`
		Status        string `json:"status"`
		KnownTargetID string `json:"known_target_id"`
	}
	var candidates []discoveredCandidate
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/discovery", "", nil, &candidates)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 discovery candidates, got %+v", candidates)
	}
	if candidates[0].IPAddress != "10.10.10.3" || candidates[1].IPAddress != "10.10.10.22" || candidates[2].IPAddress != "10.10.10.100" {
		t.Fatalf("unexpected discovery order: %+v", candidates)
	}
	if candidates[1].Status != "known" || candidates[1].KnownTargetID != knownTarget.ID {
		t.Fatalf("expected known device match for 10.10.10.22, got %+v", candidates[1])
	}
	if candidates[1].DisplayName != "MiscServer" || candidates[1].Hostname != "miscserver.local" {
		t.Fatalf("expected known device identity to be reused, got %+v", candidates[1])
	}

	req := httptest.NewRequest(http.MethodPost, "/api/discovery/"+strconv.FormatInt(candidates[1].ID, 10)+"/promote", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected known device promote to fail with 400, got %d body %q", rec.Code, rec.Body.String())
	}
}

func TestAgentPolicyIsUnmanagedWithoutAccess(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var bootstrap shared.BootstrapResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/bootstrap/register", "", shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "node-1",
		OSName:         "Linux",
		AgentVersion:   "test",
	}, &bootstrap)
	if bootstrap.AgentToken == "" {
		t.Fatalf("expected agent token")
	}

	var policy shared.AgentPolicyResponse
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/policy", bootstrap.AgentToken, nil, &policy)
	if policy.ManagedAccountEnabled || policy.AccessMode != shared.AccessModeAudit || policy.AccountState != "unmanaged" {
		t.Fatalf("expected unmanaged policy without access plugin, got %+v", policy)
	}
	if policy.ManagedUser != shared.DefaultManagedUser {
		t.Fatalf("expected default managed user %q, got %q", shared.DefaultManagedUser, policy.ManagedUser)
	}
}

func TestAgentPolicyUsesConfiguredManagedUserAndGroups(t *testing.T) {
	app := newTestAppWithConfig(t, Config{
		DBPath:        filepath.Join(t.TempDir(), "insylus.db"),
		ManagedUser:   "operator",
		ManagedGroups: []string{"adm", " adm ", "wheel", ""},
	})
	defer app.Close()

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var bootstrap shared.BootstrapResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/bootstrap/register", "", shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "node-1",
		OSName:         "Linux",
		AgentVersion:   "test",
	}, &bootstrap)

	var policy shared.AgentPolicyResponse
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/policy", bootstrap.AgentToken, nil, &policy)
	if policy.ManagedUser != "operator" {
		t.Fatalf("expected configured managed user, got %q", policy.ManagedUser)
	}
	if got, want := policy.ManagedGroups, []string{"adm", "wheel"}; !sameStrings(got, want) {
		t.Fatalf("managed groups = %+v, want %+v", got, want)
	}
	if policy.AuthorizedKeysPath != "/home/operator/.ssh/authorized_keys" {
		t.Fatalf("unexpected authorized_keys path: %q", policy.AuthorizedKeysPath)
	}
}

func TestUninstallAgentPageUsesReportedInstallPaths(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var bootstrap shared.BootstrapResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/bootstrap/register", "", shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "node-1",
		OSName:         "Linux",
		AgentVersion:   "test",
	}, &bootstrap)

	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/checkin", bootstrap.AgentToken, shared.CheckInRequest{
		Health: shared.HealthSnapshot{
			Hostname:     "node-1",
			OSName:       "Linux",
			AgentVersion: "test",
			IPs:          []string{"10.0.0.10"},
		},
		AgentInstall: shared.AgentInstallPaths{
			BinaryPath:  "/custom/bin/agent",
			ConfigPath:  "/custom/etc/config.json",
			ServiceName: "custom-agent.service",
			UnitPath:    "/custom/systemd/custom-agent.service",
		},
	}, nil)
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/report", bootstrap.AgentToken, shared.DeviceReport{
		DeviceID: device.ID,
		AgentInstall: shared.AgentInstallPaths{
			BinaryPath:  "/custom/bin/agent",
			ConfigPath:  "/custom/etc/config.json",
			ServiceName: "custom-agent.service",
			UnitPath:    "/custom/systemd/custom-agent.service",
		},
	}, nil)

	record, err := app.store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice after checkin: %v", err)
	}
	if record.Install.BinaryPath != "/custom/bin/agent" || record.Install.ServiceName != "custom-agent.service" {
		t.Fatalf("install state not saved: %+v", record.Install)
	}

	page := httptest.NewRecorder()
	app.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/devices/"+device.ID+"/uninstall", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("GET uninstall page status = %d, body %q", page.Code, page.Body.String())
	}
	if !bytes.Contains(page.Body.Bytes(), []byte("/custom/bin/agent")) || !bytes.Contains(page.Body.Bytes(), []byte("custom-agent.service")) {
		t.Fatalf("uninstall page did not render reported paths: %s", page.Body.String())
	}

	script := httptest.NewRecorder()
	app.Handler().ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/devices/"+device.ID+"/uninstall.sh", nil))
	if script.Code != http.StatusOK {
		t.Fatalf("GET uninstall script status = %d, body %q", script.Code, script.Body.String())
	}
	if !bytes.Contains(script.Body.Bytes(), []byte(`AGENT_BIN="/custom/bin/agent"`)) || !bytes.Contains(script.Body.Bytes(), []byte(`AGENT_SERVICE="custom-agent.service"`)) {
		t.Fatalf("uninstall script did not use reported paths: %s", script.Body.String())
	}
}

func TestAgentPolicyUsesPersistedManagedAccountSettings(t *testing.T) {
	app := newTestAppWithConfig(t, Config{
		DBPath:        filepath.Join(t.TempDir(), "insylus.db"),
		ManagedUser:   "operator",
		ManagedGroups: []string{"adm"},
	})
	defer app.Close()
	if err := app.store.SetManagedAccountConfig(httptest.NewRequest(http.MethodGet, "/", nil).Context(), shared.ManagedAccountConfig{
		ManagedUser: "remote",
		AccessMode:  shared.AccessModeSudoPrompted,
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig: %v", err)
	}

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var bootstrap shared.BootstrapResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/bootstrap/register", "", shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "node-1",
		OSName:         "Linux",
		AgentVersion:   "test",
	}, &bootstrap)

	var policy shared.AgentPolicyResponse
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/policy", bootstrap.AgentToken, nil, &policy)
	if policy.ManagedUser != "remote" {
		t.Fatalf("expected persisted managed user, got %q", policy.ManagedUser)
	}
	if policy.AccessMode != shared.AccessModeSudoPrompted {
		t.Fatalf("expected access mode sudo_prompted, got %q", policy.AccessMode)
	}
}

func TestSettingsManagedAccountFormUpdatesAgentPolicy(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath:        filepath.Join(t.TempDir(), "insylus.db"),
		ManagedUser:   "operator",
		ManagedGroups: []string{"adm"},
	}, "access")
	defer app.Close()

	form := url.Values{
		"managed_user": {"remote"},
		"access_level": {"sudo_passwordless"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/managed-account", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("settings update status = %d, body %q", rec.Code, rec.Body.String())
	}

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var bootstrap shared.BootstrapResponse
	doJSONRequest(t, app.Handler(), http.MethodPost, "/api/bootstrap/register", "", shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "node-1",
		OSName:         "Linux",
		AgentVersion:   "test",
	}, &bootstrap)

	var policy shared.AgentPolicyResponse
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/policy", bootstrap.AgentToken, nil, &policy)
	if policy.ManagedUser != "remote" {
		t.Fatalf("expected settings managed user, got %q", policy.ManagedUser)
	}
	if policy.AccessMode != shared.AccessModeSudoPasswordless {
		t.Fatalf("expected access mode sudo_passwordless, got %q", policy.AccessMode)
	}
}

func TestAccessSettingsPageAndFormAlias(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath:      filepath.Join(t.TempDir(), "insylus.db"),
		ManagedUser: "operator",
	}, "access")
	defer app.Close()

	page := httptest.NewRecorder()
	app.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/access/settings", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("GET /access/settings status = %d, body %q", page.Code, page.Body.String())
	}
	if !bytes.Contains(page.Body.Bytes(), []byte("Remote Access Defaults")) {
		t.Fatalf("access settings page did not render managed account settings: %s", page.Body.String())
	}

	form := url.Values{
		"managed_user":   {"accessuser"},
		"managed_groups": {"adm"},
	}
	req := httptest.NewRequest(http.MethodPost, "/access/settings/managed-account", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/access/settings" {
		t.Fatalf("access settings update status/location = %d/%q, body %q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}

func TestAgentSettingsOwnsAutoUpdatePolicy(t *testing.T) {
	app := newTestAppWithEnabledPlugins(t, Config{
		DBPath: filepath.Join(t.TempDir(), "insylus.db"),
	}, "agent")
	defer app.Close()

	page := httptest.NewRecorder()
	app.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/agent/settings", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("GET /agent/settings status = %d, body %q", page.Code, page.Body.String())
	}
	if !bytes.Contains(page.Body.Bytes(), []byte("Global Auto-Update Policy")) {
		t.Fatalf("agent settings page did not render auto-update policy: %s", page.Body.String())
	}

	form := url.Values{"agent_auto_update_default": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/agent/settings/auto-update", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agent/settings" {
		t.Fatalf("agent settings update status/location = %d/%q, body %q", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}

func TestDevicesAPIUsesInventoryOutputShape(t *testing.T) {
	app := newTestApp(t)
	defer app.Close()

	device, err := app.store.CreateDevice(httptest.NewRequest(http.MethodGet, "/", nil).Context(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	var compact []shared.DeviceInventoryCompact
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/devices?view=compact", "", nil, &compact)
	if len(compact) != 1 || compact[0].Name != "node-1" {
		t.Fatalf("unexpected compact inventory: %+v", compact)
	}

	var info shared.DeviceInventoryInfo
	doJSONRequest(t, app.Handler(), http.MethodGet, "/api/devices/"+device.ID+"?view=info", "", nil, &info)
	if info.Identity.Name != "node-1" || info.Access.ManagedUser == "" {
		t.Fatalf("unexpected info inventory: %+v", info)
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	return newTestAppWithConfig(t, Config{DBPath: filepath.Join(t.TempDir(), "insylus.db")})
}

func newTestAppWithConfig(t *testing.T, cfg Config) *App {
	t.Helper()
	app, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("New app: %v", err)
	}
	return app
}

func newTestAppWithEnabledPlugins(t *testing.T, cfg Config, enabledPluginIDs ...string) *App {
	t.Helper()
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "insylus.db")
	}
	store, err := OpenStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.EnsurePluginSettings(context.Background(), registry.Plugins()); err != nil {
		_ = store.Close()
		t.Fatalf("EnsurePluginSettings: %v", err)
	}
	for _, id := range enabledPluginIDs {
		if err := store.SetPluginEnabled(context.Background(), id, true); err != nil {
			_ = store.Close()
			t.Fatalf("SetPluginEnabled(%q): %v", id, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}
	return newTestAppWithConfig(t, cfg)
}

func newTestAppWithOnlyPlugins(t *testing.T, cfg Config, enabledPluginIDs ...string) *App {
	t.Helper()
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "insylus.db")
	}
	store, err := OpenStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.EnsurePluginSettings(context.Background(), registry.Plugins()); err != nil {
		_ = store.Close()
		t.Fatalf("EnsurePluginSettings: %v", err)
	}
	enabled := map[string]bool{}
	for _, id := range enabledPluginIDs {
		enabled[id] = true
	}
	for _, plugin := range registry.Plugins() {
		if err := store.SetPluginEnabled(context.Background(), plugin.ID(), enabled[plugin.ID()]); err != nil {
			_ = store.Close()
			t.Fatalf("SetPluginEnabled(%q): %v", plugin.ID(), err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}
	return newTestAppWithConfig(t, cfg)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func pluginEnabled(plugins []pluginhost.PluginManifest, id string) bool {
	for _, plugin := range plugins {
		if plugin.ID == id {
			return plugin.Enabled
		}
	}
	return false
}

func doJSONRequest(t *testing.T, handler http.Handler, method, path, token string, requestBody any, responseBody any) {
	t.Helper()
	var body io.Reader
	if requestBody != nil {
		data, err := json.Marshal(requestBody)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		body = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, body)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		t.Fatalf("request %s %s failed: %d %s", method, path, rec.Code, rec.Body.String())
	}
	if responseBody != nil {
		if err := json.NewDecoder(rec.Body).Decode(responseBody); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	}
}

func responseStatus(handler http.Handler, method, path string, body io.Reader) int {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}
