package server

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	"insylus/internal/shared"
	"insylus/internal/version"
)

type agentControllerService struct {
	app *App
}

func (s agentControllerService) PolicyForDevice(ctx context.Context, baseURL string, device shared.Device, goos, goarch string) (shared.AgentPolicyResponse, error) {
	policy, err := s.app.store.GetPolicyForDevice(ctx, device.ID)
	if err != nil {
		if err != sql.ErrNoRows {
			return shared.AgentPolicyResponse{}, err
		}
		policy = shared.AgentPolicyResponse{
			DeviceID:              device.ID,
			DeviceMode:            shared.DeviceModeInventoryOnly,
			ManagedAccountEnabled: false,
			AccountState:          "unmanaged",
			PolicyRevision:        1,
			FetchedAt:             time.Now().UTC(),
		}
	}
	policy, err = s.app.withManagedAccountPolicy(ctx, policy)
	if err != nil {
		return shared.AgentPolicyResponse{}, err
	}
	manifest, err := s.agentUpdateManifest(ctx, baseURL, device, goos, goarch)
	if err != nil {
		return shared.AgentPolicyResponse{}, err
	}
	policy.AgentUpdate = manifest
	return policy, nil
}

func (s agentControllerService) SaveAgentUpdateStatus(ctx context.Context, deviceID string, report shared.AgentUpdateReport) error {
	return s.app.store.SaveAgentUpdateStatus(ctx, deviceID, report)
}

func (s agentControllerService) SaveReport(ctx context.Context, token string, report shared.DeviceReport) error {
	return s.app.store.SaveReport(ctx, token, report)
}

func (s agentControllerService) agentUpdateManifest(ctx context.Context, baseURL string, device shared.Device, goos, goarch string) (shared.AgentUpdateManifest, error) {
	manifest := shared.AgentUpdateManifest{
		ServerAgentVersion: version.AgentVersion,
		GOOS:               strings.TrimSpace(goos),
		GOARCH:             strings.TrimSpace(goarch),
		Status:             shared.AgentUpdateStatusIdle,
	}
	globalEnabled, err := s.app.store.AgentAutoUpdateDefault(ctx)
	if err != nil {
		return manifest, err
	}
	record, err := s.app.store.GetDevice(ctx, device.ID)
	if err != nil {
		return manifest, err
	}
	enabled := effectiveAgentAutoUpdate(globalEnabled, record.Update.Override)
	available := compareVersions(device.AgentVersion, version.AgentVersion) < 0
	status := shared.AgentUpdateStatusIdle
	errText := ""
	if available {
		status = shared.AgentUpdateStatusAvailable
	}
	if enabled && (manifest.GOOS == "" || manifest.GOARCH == "") {
		status = shared.AgentUpdateStatusUnsupported
		errText = "agent did not provide goos/goarch"
		enabled = false
	} else if enabled && s.app.cfg.AgentBinaryPath == "" {
		status = shared.AgentUpdateStatusUnsupported
		errText = "agent binary path not configured"
		enabled = false
	} else if enabled {
		agentPath, err := resolveAgentBinaryPath(s.app.cfg.AgentBinaryPath, manifest.GOOS, manifest.GOARCH)
		if err != nil {
			status = shared.AgentUpdateStatusUnsupported
			errText = err.Error()
			enabled = false
		} else {
			sum, err := fileSHA256(agentPath)
			if err != nil {
				return manifest, err
			}
			manifest.DownloadURL = strings.TrimRight(baseURL, "/") + "/downloads/insylus-agent?goos=" + url.QueryEscape(manifest.GOOS) + "&goarch=" + url.QueryEscape(manifest.GOARCH)
			manifest.SHA256 = sum
		}
	}
	manifest.Enabled = enabled
	manifest.Status = status
	manifest.Error = errText
	if err := s.app.store.RecordAgentUpdateCheck(ctx, device.ID, enabled, available, version.AgentVersion, string(status), errText, manifest.GOOS, manifest.GOARCH); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func effectiveAgentAutoUpdate(globalEnabled bool, override shared.AgentAutoUpdateOverride) bool {
	switch override {
	case shared.AgentAutoUpdateEnabled:
		return true
	case shared.AgentAutoUpdateDisabled:
		return false
	default:
		return globalEnabled
	}
}

func (a *App) withManagedAccountPolicy(ctx context.Context, policy shared.AgentPolicyResponse) (shared.AgentPolicyResponse, error) {
	cfg, err := a.ManagedAccountConfig(ctx)
	if err != nil {
		return policy, err
	}
	user := cfg.ManagedUser
	policy.ManagedUser = user
	if policy.AccessMode == "" || policy.DeviceMode == shared.DeviceModeInventoryOnly {
		policy.AccessMode = cfg.AccessMode
	}
	policy.ManagedGroups = managedGroupsForAccessMode(policy.AccessMode, cfg.ManagedGroups)
	policy.SudoersPath = "/etc/sudoers.d/insylus-" + user
	policy.AuditReadmePath = "/etc/sudoers.d/insylus-" + user + "-audit-readme"
	policy.AuthorizedKeysPath = "/home/" + user + "/.ssh/authorized_keys"
	switch {
	case policy.DeviceMode == shared.DeviceModeInventoryOnly:
		policy.AccountState = "unmanaged"
	case !policy.ManagedAccountEnabled || policy.AccessMode == shared.AccessModeDisabled:
		policy.AccountState = "disabled"
	default:
		policy.AccountState = "enabled"
	}
	policy.FetchedAt = time.Now().UTC()
	return policy, nil
}
