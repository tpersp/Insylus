package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"insylus/internal/shared"
	"insylus/internal/version"
)

func TestCreateSSHKeyStoresFingerprint(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	key, err := store.CreateSSHKey(context.Background(), "operator-laptop", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAxWnKk4PSJVd0v8wM4OMwph0l7Qtf8f4lA5lUwWfKcG operator@atlas")
	if err != nil {
		t.Fatalf("CreateSSHKey: %v", err)
	}
	if key.Fingerprint == "" {
		t.Fatal("expected fingerprint")
	}
}

func TestUpdatePolicyIncrementsRevision(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	key, err := store.CreateSSHKey(context.Background(), "atlas", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAxWnKk4PSJVd0v8wM4OMwph0l7Qtf8f4lA5lUwWfKcG operator@atlas")
	if err != nil {
		t.Fatalf("CreateSSHKey: %v", err)
	}
	if err := store.UpdatePolicy(context.Background(), device.ID, true, shared.AccessModeAudit, &key.ID); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}

	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Policy.PolicyRevision != 2 {
		t.Fatalf("expected revision 2, got %d", record.Policy.PolicyRevision)
	}
	if !record.Policy.ManagedAccountEnabled || record.Policy.AccessMode != shared.AccessModeAudit {
		t.Fatalf("unexpected policy: %+v", record.Policy)
	}
}

func TestCreateDeviceRejectsDuplicateNameCaseInsensitive(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if _, err := store.CreateDevice(context.Background(), "MiscServer"); err != nil {
		t.Fatalf("CreateDevice first: %v", err)
	}
	if _, err := store.CreateDevice(context.Background(), "miscserver"); !errors.Is(err, ErrDuplicateDeviceName) {
		t.Fatalf("expected ErrDuplicateDeviceName, got %v", err)
	}
}

func TestNewDevicesDefaultToInventoryOnly(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "Atlas")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Policy.DeviceMode != shared.DeviceModeInventoryOnly {
		t.Fatalf("expected inventory-only default, got %+v", record.Policy)
	}
}

func TestManagedAccountConfigUsesFallbackAndPersistedSettings(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	fallback := shared.ManagedAccountConfig{ManagedUser: "operator", ManagedGroups: []string{"adm", "wheel"}}
	cfg, err := store.ManagedAccountConfig(context.Background(), fallback)
	if err != nil {
		t.Fatalf("ManagedAccountConfig fallback: %v", err)
	}
	if cfg.ManagedUser != "operator" || cfg.AccessMode != shared.AccessModeAudit {
		t.Fatalf("unexpected fallback config: %+v", cfg)
	}

	if err := store.SetManagedAccountConfig(context.Background(), shared.ManagedAccountConfig{
		ManagedUser:     "remote",
		AccessMode:      shared.AccessModeDocker,
		ManagedPassword: "correct horse battery staple",
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig: %v", err)
	}
	cfg, err = store.ManagedAccountConfig(context.Background(), fallback)
	if err != nil {
		t.Fatalf("ManagedAccountConfig persisted: %v", err)
	}
	if cfg.ManagedUser != "remote" || cfg.AccessMode != shared.AccessModeDocker {
		t.Fatalf("unexpected persisted config: %+v", cfg)
	}
	if hasString(cfg.ManagedGroups, "docker") {
		t.Fatalf("managed account config should persist base audit groups, got %+v", cfg.ManagedGroups)
	}
	if !cfg.ManagedPasswordConfigured || cfg.ManagedPassword != "correct horse battery staple" {
		t.Fatalf("unexpected managed password state: configured=%v password=%q", cfg.ManagedPasswordConfigured, cfg.ManagedPassword)
	}
	groups := managedGroupsForAccessMode(shared.AccessModeDocker, cfg.ManagedGroups)
	if !hasString(groups, "docker") || !hasString(groups, "adm") {
		t.Fatalf("docker access groups = %+v, want audit groups plus docker", groups)
	}

	if err := store.SetManagedAccountConfig(context.Background(), shared.ManagedAccountConfig{
		ManagedUser: "remote",
		AccessMode:  shared.AccessModeAudit,
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig preserve password: %v", err)
	}
	cfg, err = store.ManagedAccountConfig(context.Background(), fallback)
	if err != nil {
		t.Fatalf("ManagedAccountConfig preserved password: %v", err)
	}
	if !cfg.ManagedPasswordConfigured || cfg.ManagedPassword != "correct horse battery staple" {
		t.Fatalf("expected managed password to be preserved, got configured=%v password=%q", cfg.ManagedPasswordConfigured, cfg.ManagedPassword)
	}

	if err := store.SetManagedAccountConfig(context.Background(), shared.ManagedAccountConfig{
		ManagedUser:          "remote",
		AccessMode:           shared.AccessModeAudit,
		ClearManagedPassword: true,
	}); err != nil {
		t.Fatalf("SetManagedAccountConfig clear password: %v", err)
	}
	cfg, err = store.ManagedAccountConfig(context.Background(), fallback)
	if err != nil {
		t.Fatalf("ManagedAccountConfig cleared password: %v", err)
	}
	if cfg.ManagedPasswordConfigured || cfg.ManagedPassword != "" {
		t.Fatalf("expected managed password to be cleared, got configured=%v password=%q", cfg.ManagedPasswordConfigured, cfg.ManagedPassword)
	}
}

func TestSetDeviceModeAccessManagedStartsDisabled(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "Node01")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	key, err := store.CreateSSHKey(context.Background(), "atlas", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAxWnKk4PSJVd0v8wM4OMwph0l7Qtf8f4lA5lUwWfKcG operator@atlas")
	if err != nil {
		t.Fatalf("CreateSSHKey: %v", err)
	}
	if err := store.UpdatePolicy(context.Background(), device.ID, true, shared.AccessModeSudoPasswordless, &key.ID); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	if err := store.SetDeviceMode(context.Background(), device.ID, shared.DeviceModeAccessManaged); err != nil {
		t.Fatalf("SetDeviceMode: %v", err)
	}
	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Policy.DeviceMode != shared.DeviceModeAccessManaged || record.Policy.ManagedAccountEnabled || record.Policy.AccessMode != shared.AccessModeDisabled || record.Policy.SSHKeyID != nil {
		t.Fatalf("expected access-managed to start disabled, got %+v", record.Policy)
	}
}

func TestUpdateCheckInClearsStaleAgentUpdateFailureWhenCurrent(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := store.RecordAgentUpdateCheck(context.Background(), device.ID, true, true, version.AgentVersion, string(shared.AgentUpdateStatusAvailable), "", "linux", "amd64"); err != nil {
		t.Fatalf("RecordAgentUpdateCheck: %v", err)
	}
	if err := store.SaveAgentUpdateStatus(context.Background(), device.ID, shared.AgentUpdateReport{
		Status:             shared.AgentUpdateStatusFailed,
		Error:              `validate updated agent version: got "0.1.15" want "` + version.AgentVersion + `"`,
		ServerAgentVersion: version.AgentVersion,
		AttemptedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveAgentUpdateStatus: %v", err)
	}

	if err := store.UpdateCheckIn(context.Background(), device.ID, shared.HealthSnapshot{
		Hostname:     "miscserver",
		IPs:          []string{"10.10.10.22"},
		AgentVersion: version.AgentVersion,
		AgentGOOS:    "linux",
		AgentGOARCH:  "amd64",
	}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn: %v", err)
	}

	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Update.Status != shared.AgentUpdateStatusIdle {
		t.Fatalf("expected stale update status to reset to idle, got %q", record.Update.Status)
	}
	if record.Update.Error != "" {
		t.Fatalf("expected stale update error to be cleared, got %q", record.Update.Error)
	}
	if record.Update.UpdateAvailable {
		t.Fatal("expected update_available to be false after current-version check-in")
	}
}

func TestUpdateCheckInPersistsHealthSnapshot(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	err = store.UpdateCheckIn(context.Background(), device.ID, shared.HealthSnapshot{
		Hostname:     "miscserver",
		OSName:       "Ubuntu",
		IPs:          []string{"10.10.10.22"},
		Uptime:       "123s",
		LoadAverage:  "0.10 0.20 0.30",
		MemoryUsed:   "42.0%",
		DiskUsed:     "65.0%",
		AgentVersion: version.AgentVersion,
		AgentGOOS:    "linux",
		AgentGOARCH:  "amd64",
	}, shared.AgentInstallPaths{})
	if err != nil {
		t.Fatalf("UpdateCheckIn: %v", err)
	}

	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Report.LastPolicyHealth.Uptime != "123s" {
		t.Fatalf("expected uptime to persist from check-in, got %q", record.Report.LastPolicyHealth.Uptime)
	}
	if record.Report.LastPolicyHealth.LoadAverage != "0.10 0.20 0.30" {
		t.Fatalf("expected load to persist from check-in, got %q", record.Report.LastPolicyHealth.LoadAverage)
	}
	if record.Report.LastPolicyHealth.MemoryUsed != "42.0%" || record.Report.LastPolicyHealth.DiskUsed != "65.0%" {
		t.Fatalf("expected memory/disk to persist from check-in, got %+v", record.Report.LastPolicyHealth)
	}
}

func TestSaveReportDoesNotClobberCheckInHealthWithEmptySnapshot(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	bootstrap, err := store.RegisterAgent(context.Background(), shared.BootstrapRequest{
		BootstrapToken: device.BootstrapToken,
		Hostname:       "miscserver",
		OSName:         "Ubuntu",
		AgentVersion:   version.AgentVersion,
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), device.ID, shared.HealthSnapshot{
		Hostname:     "miscserver",
		OSName:       "Ubuntu",
		IPs:          []string{"10.10.10.22"},
		Uptime:       "123s",
		LoadAverage:  "0.10 0.20 0.30",
		MemoryUsed:   "42.0%",
		DiskUsed:     "65.0%",
		AgentVersion: version.AgentVersion,
	}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn: %v", err)
	}
	if err := store.SaveReport(context.Background(), bootstrap.AgentToken, shared.DeviceReport{
		DeviceID:         device.ID,
		LastPolicyHealth: shared.HealthSnapshot{},
	}); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}

	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Report.LastPolicyHealth.LoadAverage != "0.10 0.20 0.30" || record.Report.LastPolicyHealth.MemoryUsed != "42.0%" {
		t.Fatalf("expected check-in health to remain intact, got %+v", record.Report.LastPolicyHealth)
	}
}

func TestMigrateBackfillsLegacyReportRowsAndHealthColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open legacy db: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`create table devices (
			id text primary key,
			name text not null,
			bootstrap_token text not null unique,
			agent_token text not null default '',
			hostname text not null default '',
			os_name text not null default '',
			ips_json text not null default '[]',
			agent_version text not null default '',
			last_seen_at text,
			created_at text not null,
			updated_at text not null
		);`,
		`create table device_access_policies (
			device_id text primary key references devices(id) on delete cascade,
			access_mode text not null default 'disabled',
			ssh_key_id integer,
			policy_revision integer not null default 1,
			updated_at text not null
		);`,
		`create table device_reports (
			device_id text primary key references devices(id) on delete cascade,
			applied_revision integer not null default 0,
			user_present integer not null default 0,
			sudo_enabled integer not null default 0,
			audit_enabled integer not null default 0,
			authorized_fingerprints_json text not null default '[]',
			enforcement_succeeded integer not null default 0,
			error_message text not null default '',
			updated_at text not null
		);`,
		`create table device_metadata (
			device_id text primary key references devices(id) on delete cascade,
			note text not null default '',
			type_override text,
			parent_override_device_id text references devices(id) on delete set null,
			parent_override_state text not null default 'inherit',
			updated_at text not null
		);`,
		`create table device_discovery_snapshots (
			device_id text primary key references devices(id) on delete cascade,
			device_type text not null default 'unknown',
			platform_class text not null default 'unknown',
			workloads_json text not null default '[]',
			child_candidates_json text not null default '[]',
			warnings_json text not null default '[]',
			updated_at text not null
		);`,
		`create table relationship_candidates (
			child_device_id text primary key references devices(id) on delete cascade,
			parent_device_id text references devices(id) on delete cascade,
			confidence text not null default '',
			reason text not null default '',
			updated_at text not null
		);`,
		`create table app_settings (
			key text primary key,
			value text not null,
			updated_at text not null
		);`,
		`create table targets (
			id text primary key,
			name text not null,
			kind text not null default 'target',
			hostname text not null default '',
			ips_json text not null default '[]',
			tags_json text not null default '[]',
			note text not null default '',
			created_by text not null default '',
			created_at text not null,
			updated_at text not null
		);`,
		`create table target_addresses (
			target_id text not null references targets(id) on delete cascade,
			kind text not null,
			value text not null,
			created_at text not null,
			updated_at text not null,
			primary key(target_id, kind)
		);`,
		`create table target_metadata (
			target_id text not null references targets(id) on delete cascade,
			plugin_id text not null,
			metadata_json text not null default '{}',
			updated_at text not null,
			primary key(target_id, plugin_id)
		);`,
		`create table plugin_settings (
			plugin_id text primary key,
			enabled integer not null default 0,
			settings_json text not null default '{}',
			created_at text not null,
			updated_at text not null
		);`,
		`create table plugin_secrets (
			plugin_id text not null,
			name text not null,
			ciphertext text not null,
			created_at text not null,
			updated_at text not null,
			primary key(plugin_id, name)
		);`,
		`create table device_agent_updates (
			device_id text primary key references devices(id) on delete cascade,
			auto_update_override text not null default 'inherit',
			effective_enabled integer not null default 0,
			update_available integer not null default 0,
			server_agent_version text not null default '',
			status text not null default 'idle',
			error text not null default '',
			last_checked_at text,
			last_attempted_at text,
			reported_goos text not null default '',
			reported_goarch text not null default '',
			updated_at text not null
		);`,
		`create table topology_nodes (
			id integer primary key autoincrement,
			name text not null,
			kind text not null,
			note text not null default '',
			created_at text not null,
			updated_at text not null
		);`,
		`create table topology_links (
			id integer primary key autoincrement,
			from_kind text not null,
			from_id text not null,
			to_kind text not null,
			to_id text not null,
			label text not null default '',
			source text not null default 'manual',
			created_at text not null,
			updated_at text not null
		);`,
		`create table service_instances (
			id integer primary key autoincrement,
			device_id text not null references devices(id) on delete cascade,
			normalized_name text not null,
			name text not null,
			kind text not null,
			image text not null default '',
			state text not null default '',
			discovered_state text not null default '',
			health text not null default 'unknown',
			endpoints_json text not null default '[]',
			first_seen_at text not null,
			last_seen_at text,
			missing_since text,
			last_reported_at text not null,
			created_at text not null,
			updated_at text not null
		);`,
		`create table service_events (
			id integer primary key autoincrement,
			service_instance_id integer,
			device_id text not null references devices(id) on delete cascade,
			service_name text not null,
			service_kind text not null,
			action text not null,
			details text not null default '',
			created_at text not null
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec legacy stmt failed: %v\n%s", err, stmt)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`insert into devices (id, name, bootstrap_token, created_at, updated_at) values (?, ?, ?, ?, ?)`, "dev1", "MiscServer", "boot", now, now); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err := db.Exec(`insert into targets (id, name, created_at, updated_at) values (?, ?, ?, ?)`, "dev1", "MiscServer", now, now); err != nil {
		t.Fatalf("insert target: %v", err)
	}
	if _, err := db.Exec(`insert into device_access_policies (device_id, access_mode, policy_revision, updated_at) values (?, 'disabled', 1, ?)`, "dev1", now); err != nil {
		t.Fatalf("insert legacy policy: %v", err)
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore migrated legacy db: %v", err)
	}
	defer store.Close()

	if err := store.UpdateCheckIn(context.Background(), "dev1", shared.HealthSnapshot{
		Hostname:     "miscserver",
		OSName:       "Ubuntu",
		IPs:          []string{"10.10.10.22"},
		Uptime:       "123s",
		LoadAverage:  "0.10 0.20 0.30",
		MemoryUsed:   "42.0%",
		DiskUsed:     "65.0%",
		AgentVersion: version.AgentVersion,
	}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn after legacy migration: %v", err)
	}

	record, err := store.GetDevice(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Report.LastPolicyHealth.LoadAverage != "0.10 0.20 0.30" || record.Report.LastPolicyHealth.MemoryUsed != "42.0%" {
		t.Fatalf("expected migrated legacy db to persist health, got %+v", record.Report.LastPolicyHealth)
	}

	var samples int
	if err := store.db.QueryRowContext(context.Background(), `select count(*) from device_health_samples where device_id = ?`, "dev1").Scan(&samples); err != nil {
		t.Fatalf("count health samples: %v", err)
	}
	if samples == 0 {
		t.Fatal("expected migrated legacy db to record health samples")
	}
}

func TestTopologyInferenceAndOverrides(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	parent, err := store.CreateDevice(context.Background(), "Alpha-pve")
	if err != nil {
		t.Fatalf("CreateDevice parent: %v", err)
	}
	child, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice child: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), parent.ID, shared.HealthSnapshot{Hostname: "alpha-pve", IPs: []string{"10.0.0.2"}}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn parent: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), child.ID, shared.HealthSnapshot{Hostname: "miscserver", IPs: []string{"10.0.0.5"}}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn child: %v", err)
	}
	if err := store.saveDiscoverySnapshot(context.Background(), parent.ID, shared.TopologyDiscovery{
		DeviceType:      shared.DeviceTypeProxmoxNode,
		PlatformClass:   shared.PlatformClassGenericLinux,
		ChildCandidates: []shared.ChildCandidate{{Name: "miscserver", Kind: shared.WorkloadKindVM, IPs: []string{"10.0.0.5"}}},
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot parent: %v", err)
	}
	if err := store.saveDiscoverySnapshot(context.Background(), child.ID, shared.TopologyDiscovery{
		DeviceType:    shared.DeviceTypeVM,
		PlatformClass: shared.PlatformClassGenericLinux,
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot child: %v", err)
	}
	if err := store.resolveTopology(context.Background()); err != nil {
		t.Fatalf("resolveTopology: %v", err)
	}

	record, err := store.GetDevice(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("GetDevice child: %v", err)
	}
	if record.Resolved.ParentDeviceID != parent.ID || record.Resolved.ParentState != shared.ParentStateLinked {
		t.Fatalf("expected linked parent, got %+v", record.Resolved)
	}

	if err := store.SetParentOverride(context.Background(), child.ID, shared.ParentOverrideManualUnknown, nil); err != nil {
		t.Fatalf("SetParentOverride: %v", err)
	}
	record, err = store.GetDevice(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("GetDevice child after override: %v", err)
	}
	if record.Resolved.ParentState != shared.ParentStateUnknown || record.Resolved.ParentSource != shared.TopologySourceManual {
		t.Fatalf("expected manual unknown parent, got %+v", record.Resolved)
	}
}

func TestManualTopologyNodesAndLinks(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "Atlas")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	router, err := store.CreateTopologyNode(context.Background(), "Router", shared.TopologyNodeKindRouter, "fiber uplink")
	if err != nil {
		t.Fatalf("CreateTopologyNode router: %v", err)
	}
	switchNode, err := store.CreateTopologyNode(context.Background(), "Basement Switch", shared.TopologyNodeKindSwitch, "")
	if err != nil {
		t.Fatalf("CreateTopologyNode switch: %v", err)
	}
	if _, err := store.CreateTopologyNode(context.Background(), "", shared.TopologyNodeKindRouter, ""); err == nil {
		t.Fatal("expected empty topology node name to fail")
	}
	if _, err := store.CreateTopologyNode(context.Background(), "Bad", shared.TopologyNodeKindDevice, ""); err == nil {
		t.Fatal("expected invalid manual topology node kind to fail")
	}

	if _, err := store.CreateTopologyLink(context.Background(), shared.TopologyEndpointTopologyNode, fmt.Sprint(router.ID), shared.TopologyEndpointTopologyNode, fmt.Sprint(switchNode.ID), "uplink"); err != nil {
		t.Fatalf("CreateTopologyLink node-node: %v", err)
	}
	if _, err := store.CreateTopologyLink(context.Background(), shared.TopologyEndpointTopologyNode, fmt.Sprint(switchNode.ID), shared.TopologyEndpointDevice, device.ID, "downlink"); err != nil {
		t.Fatalf("CreateTopologyLink node-device: %v", err)
	}
	if _, err := store.CreateTopologyLink(context.Background(), shared.TopologyEndpointTopologyNode, fmt.Sprint(router.ID), shared.TopologyEndpointTopologyNode, fmt.Sprint(switchNode.ID), "uplink"); !errors.Is(err, ErrDuplicateTopologyLink) {
		t.Fatalf("expected duplicate link error, got %v", err)
	}
	if _, err := store.CreateTopologyLink(context.Background(), shared.TopologyEndpointDevice, "missing", shared.TopologyEndpointTopologyNode, fmt.Sprint(router.ID), "bad"); err == nil {
		t.Fatal("expected missing device endpoint to fail")
	}

	nodes, err := store.ListTopologyNodes(context.Background())
	if err != nil {
		t.Fatalf("ListTopologyNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 manual nodes, got %+v", nodes)
	}
	links, err := store.ListTopologyLinks(context.Background())
	if err != nil {
		t.Fatalf("ListTopologyLinks: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 manual links, got %+v", links)
	}
	if err := store.DeleteTopologyNode(context.Background(), switchNode.ID); err != nil {
		t.Fatalf("DeleteTopologyNode: %v", err)
	}
	links, err = store.ListTopologyLinks(context.Background())
	if err != nil {
		t.Fatalf("ListTopologyLinks after delete: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected linked manual edges to be deleted with node, got %+v", links)
	}
}

func TestTopologyInferenceUniqueProxmoxNameFallback(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	parent, err := store.CreateDevice(context.Background(), "Alpha-pve")
	if err != nil {
		t.Fatalf("CreateDevice parent: %v", err)
	}
	child, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice child: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), parent.ID, shared.HealthSnapshot{Hostname: "alpha-pve", IPs: []string{"10.0.0.2"}}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn parent: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), child.ID, shared.HealthSnapshot{Hostname: "miscserver", IPs: []string{"10.0.0.5"}}, shared.AgentInstallPaths{}); err != nil {
		t.Fatalf("UpdateCheckIn child: %v", err)
	}
	if err := store.saveDiscoverySnapshot(context.Background(), parent.ID, shared.TopologyDiscovery{
		DeviceType:      shared.DeviceTypeBareMetal,
		Purpose:         shared.DevicePurposeProxmoxNode,
		PlatformClass:   shared.PlatformClassGenericLinux,
		ChildCandidates: []shared.ChildCandidate{{Name: "MiscServer", Kind: shared.WorkloadKindVM}},
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot parent: %v", err)
	}
	if err := store.saveDiscoverySnapshot(context.Background(), child.ID, shared.TopologyDiscovery{
		DeviceType:    shared.DeviceTypeVM,
		PlatformClass: shared.PlatformClassGenericLinux,
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot child: %v", err)
	}
	if err := store.resolveTopology(context.Background()); err != nil {
		t.Fatalf("resolveTopology: %v", err)
	}

	record, err := store.GetDevice(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("GetDevice child: %v", err)
	}
	if record.Resolved.ParentDeviceID != parent.ID || record.Resolved.MatchConfidence != "medium" {
		t.Fatalf("expected medium unique name fallback, got %+v", record.Resolved)
	}
}

func TestTopologyInferenceNameFallbackRequiresUniqueParent(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	alpha, err := store.CreateDevice(context.Background(), "Alpha-pve")
	if err != nil {
		t.Fatalf("CreateDevice alpha: %v", err)
	}
	beta, err := store.CreateDevice(context.Background(), "Beta-pve")
	if err != nil {
		t.Fatalf("CreateDevice beta: %v", err)
	}
	child, err := store.CreateDevice(context.Background(), "MiscServer")
	if err != nil {
		t.Fatalf("CreateDevice child: %v", err)
	}
	for _, device := range []shared.Device{alpha, beta, child} {
		if err := store.UpdateCheckIn(context.Background(), device.ID, shared.HealthSnapshot{Hostname: device.Name, IPs: []string{"10.0.0.5"}}, shared.AgentInstallPaths{}); err != nil {
			t.Fatalf("UpdateCheckIn %s: %v", device.Name, err)
		}
	}
	for _, parent := range []shared.Device{alpha, beta} {
		if err := store.saveDiscoverySnapshot(context.Background(), parent.ID, shared.TopologyDiscovery{
			DeviceType:      shared.DeviceTypeBareMetal,
			Purpose:         shared.DevicePurposeProxmoxNode,
			PlatformClass:   shared.PlatformClassGenericLinux,
			ChildCandidates: []shared.ChildCandidate{{Name: "MiscServer", Kind: shared.WorkloadKindVM}},
			UpdatedAt:       time.Now().UTC(),
		}); err != nil {
			t.Fatalf("saveDiscoverySnapshot parent: %v", err)
		}
	}
	if err := store.saveDiscoverySnapshot(context.Background(), child.ID, shared.TopologyDiscovery{
		DeviceType:    shared.DeviceTypeVM,
		PlatformClass: shared.PlatformClassGenericLinux,
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot child: %v", err)
	}
	if err := store.resolveTopology(context.Background()); err != nil {
		t.Fatalf("resolveTopology: %v", err)
	}

	record, err := store.GetDevice(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("GetDevice child: %v", err)
	}
	if record.Resolved.ParentState != shared.ParentStateUnknown {
		t.Fatalf("expected ambiguous name fallback to stay unknown, got %+v", record.Resolved)
	}
}

func TestPurposeOverrideWinsUntilCleared(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	device, err := store.CreateDevice(context.Background(), "Atlas")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := store.saveDiscoverySnapshot(context.Background(), device.ID, shared.TopologyDiscovery{
		DeviceType:    shared.DeviceTypeVM,
		Purpose:       shared.DevicePurposeUnknown,
		PlatformClass: shared.PlatformClassGenericLinux,
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot: %v", err)
	}

	purpose := shared.DevicePurpose("Coding server")
	if err := store.SetPurposeOverride(context.Background(), device.ID, &purpose); err != nil {
		t.Fatalf("SetPurposeOverride: %v", err)
	}
	record, err := store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if record.Resolved.Purpose != shared.DevicePurpose("Coding server") || record.Resolved.PurposeSource != shared.TopologySourceManual {
		t.Fatalf("expected manual custom purpose, got %+v", record.Resolved)
	}

	if err := store.saveDiscoverySnapshot(context.Background(), device.ID, shared.TopologyDiscovery{
		DeviceType:    shared.DeviceTypeVM,
		Purpose:       shared.DevicePurposeDockerHost,
		PlatformClass: shared.PlatformClassGenericLinux,
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveDiscoverySnapshot updated: %v", err)
	}
	record, err = store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice after discovery: %v", err)
	}
	if record.Resolved.Purpose != shared.DevicePurpose("Coding server") || record.Resolved.PurposeSource != shared.TopologySourceManual {
		t.Fatalf("expected manual purpose to survive discovery, got %+v", record.Resolved)
	}

	if err := store.SetPurposeOverride(context.Background(), device.ID, nil); err != nil {
		t.Fatalf("clear SetPurposeOverride: %v", err)
	}
	record, err = store.GetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("GetDevice after clear: %v", err)
	}
	if record.Resolved.Purpose != shared.DevicePurposeDockerHost || record.Resolved.PurposeSource != shared.TopologySourceDiscovered {
		t.Fatalf("expected cleared override to return to discovered purpose, got %+v", record.Resolved)
	}
}

func TestServiceInstancesPersistAndMarkMissing(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()

	device, err := store.CreateDevice(ctx, "Docker01")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	firstSeen := time.Now().UTC().Add(-2 * time.Minute)
	if err := store.syncServiceInstances(ctx, device.ID, []shared.Workload{
		{
			Name:      "jellyfin",
			Kind:      shared.WorkloadKindContainer,
			Image:     "jellyfin/jellyfin",
			State:     "running",
			Endpoints: []shared.Endpoint{{Host: "0.0.0.0", Port: "8096/tcp"}},
		},
	}, firstSeen); err != nil {
		t.Fatalf("syncServiceInstances first: %v", err)
	}
	records, err := store.ListServiceInstances(ctx)
	if err != nil {
		t.Fatalf("ListServiceInstances first: %v", err)
	}
	if len(records) != 1 || records[0].Name != "jellyfin" || records[0].Health != shared.ServiceHealthHealthy {
		t.Fatalf("unexpected first service records: %+v", records)
	}

	if err := store.syncServiceInstances(ctx, device.ID, []shared.Workload{
		{Name: "jellyfin", Kind: shared.WorkloadKindContainer, Image: "jellyfin/jellyfin", State: "running"},
	}, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("syncServiceInstances repeat: %v", err)
	}
	records, err = store.ListServiceInstances(ctx)
	if err != nil {
		t.Fatalf("ListServiceInstances repeat: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected repeat report to update one row, got %+v", records)
	}

	if err := store.syncServiceInstances(ctx, device.ID, nil, time.Now().UTC()); err != nil {
		t.Fatalf("syncServiceInstances missing: %v", err)
	}
	records, err = store.ListServiceInstances(ctx)
	if err != nil {
		t.Fatalf("ListServiceInstances missing: %v", err)
	}
	if len(records) != 1 || records[0].Health != shared.ServiceHealthMissing || records[0].MissingSince.IsZero() {
		t.Fatalf("expected missing service to persist, got %+v", records)
	}

	if err := store.syncServiceInstances(ctx, device.ID, []shared.Workload{
		{Name: "jellyfin", Kind: shared.WorkloadKindContainer, State: "running"},
	}, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("syncServiceInstances restore: %v", err)
	}
	records, err = store.ListServiceInstances(ctx)
	if err != nil {
		t.Fatalf("ListServiceInstances restore: %v", err)
	}
	if len(records) != 1 || records[0].Health != shared.ServiceHealthHealthy || !records[0].MissingSince.IsZero() {
		t.Fatalf("expected restored service to clear missing state, got %+v", records)
	}
}

func TestPruneMissingServiceInstances(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()

	device, err := store.CreateDevice(ctx, "Docker01")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := store.syncServiceInstances(ctx, device.ID, []shared.Workload{
		{Name: "jellyfin", Kind: shared.WorkloadKindContainer, Image: "jellyfin/jellyfin", State: "running"},
		{Name: "radarr", Kind: shared.WorkloadKindContainer, State: "running"},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("syncServiceInstances first: %v", err)
	}
	if err := store.syncServiceInstances(ctx, device.ID, []shared.Workload{
		{Name: "radarr", Kind: shared.WorkloadKindContainer, State: "running"},
	}, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("syncServiceInstances missing: %v", err)
	}
	deleted, err := store.PruneMissingServiceInstances(ctx, "", "jelly")
	if err != nil {
		t.Fatalf("PruneMissingServiceInstances: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted service, got %d", deleted)
	}
	records, err := store.ListServiceInstances(ctx)
	if err != nil {
		t.Fatalf("ListServiceInstances: %v", err)
	}
	if len(records) != 1 || records[0].Name != "radarr" {
		t.Fatalf("expected only radarr to remain, got %+v", records)
	}
}

func TestServiceHealthTreatsExpectedOffStatesAsNeutral(t *testing.T) {
	cases := map[string]shared.ServiceHealth{
		"stopped":           shared.ServiceHealthUnknown,
		"exited (0)":        shared.ServiceHealthUnknown,
		"inactive":          shared.ServiceHealthUnknown,
		"paused":            shared.ServiceHealthUnknown,
		"failed":            shared.ServiceHealthUnhealthy,
		"error":             shared.ServiceHealthUnhealthy,
		"running":           shared.ServiceHealthHealthy,
		"active":            shared.ServiceHealthHealthy,
		"up":                shared.ServiceHealthHealthy,
		"running (healthy)": shared.ServiceHealthHealthy,
	}

	for state, want := range cases {
		t.Run(state, func(t *testing.T) {
			if got := classifyServiceHealth(state, false); got != want {
				t.Fatalf("classifyServiceHealth(%q) = %q, want %q", state, got, want)
			}
		})
	}
	if got := classifyServiceHealth("running", true); got != shared.ServiceHealthMissing {
		t.Fatalf("missing service health = %q, want %q", got, shared.ServiceHealthMissing)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "insylus.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store
}
