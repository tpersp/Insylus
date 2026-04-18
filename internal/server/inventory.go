package server

import (
	"time"

	"insylus/internal/shared"
)

func InventoryItemFromRecord(record DeviceRecord, children []string) shared.DeviceInventoryItem {
	return InventoryItemFromRecordWithManagedUser(record, children, shared.DefaultManagedUser)
}

func InventoryItemFromRecordWithManagedUser(record DeviceRecord, children []string, managedUser string) shared.DeviceInventoryItem {
	info := InventoryInfoFromRecordWithManagedUser(record, children, managedUser)
	return shared.DeviceInventoryItem{
		Identity:   info.Identity,
		Connection: info.Connection,
		Topology:   info.Topology,
		WakeOnLAN:  info.WakeOnLAN,
		Agent:      info.Agent,
		Access:     info.Access,
		Health: shared.DeviceHealthDetails{
			Uptime:      record.Report.LastPolicyHealth.Uptime,
			LoadAverage: record.Report.LastPolicyHealth.LoadAverage,
			MemoryUsed:  record.Report.LastPolicyHealth.MemoryUsed,
			DiskUsed:    record.Report.LastPolicyHealth.DiskUsed,
		},
		Inventory: shared.DeviceDiscoveryInfo{
			Workloads:         record.Discovery.Workloads,
			DiscoveryWarnings: record.Discovery.Warnings,
		},
	}
}

func InventoryCompactFromRecord(record DeviceRecord) shared.DeviceInventoryCompact {
	return shared.DeviceInventoryCompact{
		Name:     record.Device.Name,
		Hostname: record.Device.Hostname,
		IPs:      record.Device.IPs,
		Purpose:  compactPurpose(record.Resolved.Purpose),
	}
}

func compactPurpose(purpose shared.DevicePurpose) string {
	if purpose == "" || purpose == shared.DevicePurposeUnknown {
		return ""
	}
	return string(purpose)
}

func InventoryInfoFromRecord(record DeviceRecord, children []string) shared.DeviceInventoryInfo {
	return InventoryInfoFromRecordWithManagedUser(record, children, shared.DefaultManagedUser)
}

func InventoryInfoFromRecordWithManagedUser(record DeviceRecord, children []string, managedUser string) shared.DeviceInventoryInfo {
	alias, sshCommand := managedConnection(record)
	return shared.DeviceInventoryInfo{
		Identity: shared.DeviceIdentity{
			ID:         record.Device.ID,
			Name:       record.Device.Name,
			Hostname:   record.Device.Hostname,
			OSName:     record.Device.OSName,
			IPs:        record.Device.IPs,
			LastSeenAt: record.Device.LastSeenAt,
			Note:       record.Metadata.Note,
		},
		Connection: shared.DeviceConnection{
			SSHAlias:   alias,
			SSHCommand: sshCommand,
		},
		WakeOnLAN: record.Discovery.WakeOnLAN,
		Topology: shared.DeviceTopologyInfo{
			DeviceType:       record.Resolved.EffectiveDeviceType,
			DeviceTypeSource: record.Resolved.DeviceTypeSource,
			Purpose:          record.Resolved.Purpose,
			PurposeSource:    record.Resolved.PurposeSource,
			PlatformClass:    record.Resolved.PlatformClass,
			ParentDeviceID:   record.Resolved.ParentDeviceID,
			ParentName:       record.Resolved.ParentName,
			ParentState:      record.Resolved.ParentState,
			ParentSource:     record.Resolved.ParentSource,
			Children:         children,
			LastDiscoveredAt: record.Discovery.UpdatedAt,
		},
		Access: shared.DeviceAccessInfo{
			DeviceMode:            record.Policy.DeviceMode,
			ManagedAccountEnabled: record.Policy.ManagedAccountEnabled,
			ManagedUser:           managedUser,
			AccessMode:            record.Policy.AccessMode,
			AssignedKeyName:       record.Policy.AssignedKeyName,
			AssignedFingerprint:   record.Policy.Fingerprint,
			PolicyRevision:        record.Policy.PolicyRevision,
			AppliedRevision:       record.Report.AppliedRevision,
			EnforcementSucceeded:  record.Report.EnforcementSucceeded,
			ErrorMessage:          record.Report.ErrorMessage,
		},
		Agent: shared.DeviceAgentInfo{
			Version:    record.Device.AgentVersion,
			GOOS:       record.Report.LastPolicyHealth.AgentGOOS,
			GOARCH:     record.Report.LastPolicyHealth.AgentGOARCH,
			AutoUpdate: AgentUpdateInfoFromRecord(record),
		},
		Health: shared.DeviceHealthInfo{
			Uptime:      record.Report.LastPolicyHealth.Uptime,
			LoadAverage: record.Report.LastPolicyHealth.LoadAverage,
			MemoryUsed:  record.Report.LastPolicyHealth.MemoryUsed,
			DiskUsed:    record.Report.LastPolicyHealth.DiskUsed,
		},
	}
}

func managedConnection(record DeviceRecord) (string, string) {
	if !managedSSHAvailable(record) {
		return "", ""
	}
	alias := managedPrimaryAlias(record.Device.Name)
	if alias == "" {
		return "", ""
	}
	return alias, "ssh " + alias
}

func managedSSHAvailable(record DeviceRecord) bool {
	return record.Policy.DeviceMode == shared.DeviceModeAccessManaged &&
		record.Policy.ManagedAccountEnabled &&
		record.Policy.AccessMode != shared.AccessModeDisabled &&
		len(record.Device.IPs) > 0
}

func AgentUpdateInfoFromRecord(record DeviceRecord) shared.AgentUpdateInfo {
	return shared.AgentUpdateInfo{
		Enabled:         record.Update.EffectiveEnabled,
		Override:        record.Update.Override,
		UpdateAvailable: record.Update.UpdateAvailable,
		ServerVersion:   record.Update.ServerAgentVersion,
		Status:          record.Update.Status,
		Error:           record.Update.Error,
		LastCheckedAt:   timePtr(record.Update.LastCheckedAt),
		LastAttemptedAt: timePtr(record.Update.LastAttemptedAt),
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func FindMatchFromRecord(record DeviceRecord) shared.DeviceFindMatch {
	return shared.DeviceFindMatch{
		ID:       record.Device.ID,
		Name:     record.Device.Name,
		Hostname: record.Device.Hostname,
		IPs:      record.Device.IPs,
	}
}

func childNamesByParent(records []DeviceRecord) map[string][]string {
	out := map[string][]string{}
	for _, record := range records {
		if record.Resolved.ParentState != shared.ParentStateLinked || record.Resolved.ParentDeviceID == "" {
			continue
		}
		out[record.Resolved.ParentDeviceID] = append(out[record.Resolved.ParentDeviceID], record.Device.Name)
	}
	return out
}

func deviceIsOnline(lastSeen time.Time) bool {
	return !lastSeen.IsZero() && time.Since(lastSeen) <= shared.DeviceOnlineWindow
}
