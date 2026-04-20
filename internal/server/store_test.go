package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"insylus/internal/shared"
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
		ManagedUser: "remote",
		AccessMode:  shared.AccessModeDocker,
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
	groups := managedGroupsForAccessMode(shared.AccessModeDocker, cfg.ManagedGroups)
	if !hasString(groups, "docker") || !hasString(groups, "adm") {
		t.Fatalf("docker access groups = %+v, want audit groups plus docker", groups)
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
	if err := store.UpdateCheckIn(context.Background(), parent.ID, shared.HealthSnapshot{Hostname: "alpha-pve", IPs: []string{"10.0.0.2"}}); err != nil {
		t.Fatalf("UpdateCheckIn parent: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), child.ID, shared.HealthSnapshot{Hostname: "miscserver", IPs: []string{"10.0.0.5"}}); err != nil {
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
	if err := store.UpdateCheckIn(context.Background(), parent.ID, shared.HealthSnapshot{Hostname: "alpha-pve", IPs: []string{"10.0.0.2"}}); err != nil {
		t.Fatalf("UpdateCheckIn parent: %v", err)
	}
	if err := store.UpdateCheckIn(context.Background(), child.ID, shared.HealthSnapshot{Hostname: "miscserver", IPs: []string{"10.0.0.5"}}); err != nil {
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
		if err := store.UpdateCheckIn(context.Background(), device.ID, shared.HealthSnapshot{Hostname: device.Name, IPs: []string{"10.0.0.5"}}); err != nil {
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

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "insylus.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store
}
