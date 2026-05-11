package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"insylus/internal/shared"
)

type syscallStatfs = syscall.Statfs_t

const (
	insylusKeyBlockBegin = "# insylus-managed-key begin"
	insylusKeyBlockEnd   = "# insylus-managed-key end"
)

type managedAccountPolicy struct {
	User               string
	Groups             []string
	Password           string
	SudoersPath        string
	AuditReadmePath    string
	AuthorizedKeysPath string
}

func managedPolicyFromResponse(policy shared.AgentPolicyResponse) managedAccountPolicy {
	user := strings.TrimSpace(policy.ManagedUser)
	if user == "" {
		user = shared.DefaultManagedUser
	}
	groups := append([]string(nil), policy.ManagedGroups...)
	// For audit mode (or unset), ensure adm group is present; for docker mode, add docker group
	if len(groups) == 0 {
		if policy.AccessMode == "" || policy.AccessMode == shared.AccessModeAudit {
			groups = []string{"adm"}
		} else if policy.AccessMode == shared.AccessModeDocker {
			groups = []string{"docker"}
		}
		// For sudo modes, no groups needed for privilege
	}
	return managedAccountPolicy{
		User:               user,
		Groups:             groups,
		Password:           policy.ManagedPassword,
		SudoersPath:        firstManagedPolicyValue(policy.SudoersPath, "/etc/sudoers.d/insylus-"+user),
		AuditReadmePath:    firstManagedPolicyValue(policy.AuditReadmePath, "/etc/sudoers.d/insylus-"+user+"-audit-readme"),
		AuthorizedKeysPath: firstManagedPolicyValue(policy.AuthorizedKeysPath, filepath.Join(managedHomeDir(user), ".ssh", "authorized_keys")),
	}
}

func statfs(path string, stat *syscallStatfs) error {
	return syscall.Statfs(path, stat)
}

func applyPolicy(policy shared.AgentPolicyResponse) shared.DeviceReport {
	managed := managedPolicyFromResponse(policy)
	report := shared.DeviceReport{
		AppliedRevision:      policy.PolicyRevision,
		EnforcementSucceeded: true,
	}
	if policy.DeviceMode == shared.DeviceModeInventoryOnly {
		if warning, err := cleanupInventoryOnly(managed, policy.AssignedKey); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = err.Error()
		} else if warning != "" {
			report.ErrorMessage = warning
		}
		return report
	}
	if !policy.ManagedAccountEnabled || policy.AccessMode == shared.AccessModeDisabled {
		if err := disableManagedUser(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = err.Error()
		}
		return report
	}
	if err := ensureUser(managed.User); err != nil {
		report.EnforcementSucceeded = false
		report.ErrorMessage = err.Error()
		return report
	}
	if managed.Password != "" {
		if err := ensurePassword(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = "failed to set managed password"
			return report
		}
	}
	report.UserPresent = true
	if policy.AssignedKey != "" {
		if err := ensureAuthorizedKey(managed, policy.AssignedKey); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = err.Error()
			return report
		}
		report.AuthorizedFingerprints = []string{policy.KeyFingerprint}
	}
	switch policy.AccessMode {
	case shared.AccessModeSudoPasswordless:
		if err := ensureSudo(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = "failed to enable passwordless sudo"
			return report
		}
		report.SudoEnabled = true
		if err := removeAudit(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to remove audit access: %v", err)
			return report
		}
	case shared.AccessModeSudoPrompted:
		// Use system sudo (prompts for password) - don't install a sudoers file
		report.SudoEnabled = true
		if err := removeSudo(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to remove existing sudoers: %v", err)
			return report
		}
		if err := removeAudit(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to remove audit access: %v", err)
			return report
		}
	case shared.AccessModeDocker:
		if err := ensureAudit(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to enable docker access: %v", err)
			return report
		}
		report.DockerEnabled = true
		if err := removeSudo(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to remove sudo access: %v", err)
			return report
		}
	case shared.AccessModeAudit:
		if err := ensureAudit(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = "failed to enable audit access"
			return report
		}
		report.AuditEnabled = true
		if err := removeSudo(managed); err != nil {
			report.EnforcementSucceeded = false
			report.ErrorMessage = fmt.Sprintf("failed to remove sudo access: %v", err)
			return report
		}
	}
	return report
}

func ensureUser(user string) error {
	if exec.Command("id", "-u", user).Run() == nil {
		return nil
	}
	return exec.Command("useradd", "-m", "-s", "/bin/bash", user).Run()
}

func ensurePassword(managed managedAccountPolicy) error {
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(managed.User + ":" + managed.Password + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("usermod", "--unlock", managed.User).Run()
	return nil
}

func disableManagedUser(managed managedAccountPolicy) error {
	if err := errors.Join(removeSudo(managed), removeAudit(managed)); err != nil {
		return err
	}
	if exec.Command("id", "-u", managed.User).Run() != nil {
		return nil
	}
	return exec.Command("usermod", "--lock", managed.User).Run()
}

func ensureAuthorizedKey(managed managedAccountPolicy, publicKey string) error {
	keyPath := managedAuthorizedKeysPath(managed)
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	current, _ := os.ReadFile(keyPath)
	next := upsertManagedAuthorizedKeyBlock(string(current), publicKey)
	if err := os.WriteFile(keyPath, []byte(next), 0o600); err != nil {
		return err
	}
	if err := exec.Command("chown", "-R", managed.User+":"+managed.User, filepath.Dir(keyPath)).Run(); err != nil {
		return err
	}
	return nil
}

func ensureSudo(managed managedAccountPolicy) error {
	content := managed.User + " ALL=(ALL) NOPASSWD:ALL\n"
	return os.WriteFile(managed.SudoersPath, []byte(content), 0o440)
}

func removeSudo(managed managedAccountPolicy) error {
	if err := os.Remove(managed.SudoersPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ensureAudit(managed managedAccountPolicy) error {
	for _, group := range managed.Groups {
		if err := exec.Command("getent", "group", group).Run(); err != nil {
			return fmt.Errorf("%s group missing", group)
		}
	}
	if err := exec.Command("usermod", "-aG", strings.Join(managed.Groups, ","), managed.User).Run(); err != nil {
		return err
	}
	return os.WriteFile(managed.AuditReadmePath, []byte("# audit-only mode managed by insylus\n"), 0o440)
}

func removeAudit(managed managedAccountPolicy) error {
	for _, group := range managed.Groups {
		inGroup, err := userInGroup(managed.User, group)
		if err != nil {
			return err
		}
		if !inGroup {
			continue
		}
		if err := exec.Command("gpasswd", "-d", managed.User, group).Run(); err != nil {
			return err
		}
	}
	if err := os.Remove(managed.AuditReadmePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func userInGroup(user, group string) (bool, error) {
	if exec.Command("id", "-u", user).Run() != nil {
		return false, nil
	}
	out, err := exec.Command("id", "-nG", user).Output()
	if err != nil {
		return false, err
	}
	for _, existing := range strings.Fields(string(out)) {
		if existing == group {
			return true, nil
		}
	}
	return false, nil
}

func cleanupInventoryOnly(managed managedAccountPolicy, assignedKey string) (string, error) {
	if err := removeSudo(managed); err != nil {
		return "", err
	}
	if err := removeInsylusAuditReadmeOnly(managed); err != nil {
		return "", err
	}
	warning, err := removeManagedAuthorizedKey(managed, assignedKey)
	if err != nil {
		return "", err
	}
	return warning, nil
}

func removeInsylusAuditReadmeOnly(managed managedAccountPolicy) error {
	if err := os.Remove(managed.AuditReadmePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeManagedAuthorizedKey(managed managedAccountPolicy, assignedKey string) (string, error) {
	path := managedAuthorizedKeysPath(managed)
	current, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	next, changed, warning := removeManagedAuthorizedKeyContent(string(current), assignedKey)
	if !changed {
		return warning, nil
	}
	if strings.TrimSpace(next) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return warning, nil
	}
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		return "", err
	}
	return warning, nil
}

func managedHomeDir(user string) string {
	home := "/home/" + user
	if _, err := os.Stat(home); err != nil {
		home = "/var/lib/" + user
	}
	return home
}

func managedAuthorizedKeysPath(managed managedAccountPolicy) string {
	if strings.TrimSpace(managed.AuthorizedKeysPath) != "" {
		return managed.AuthorizedKeysPath
	}
	return filepath.Join(managedHomeDir(managed.User), ".ssh", "authorized_keys")
}

func firstManagedPolicyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func upsertManagedAuthorizedKeyBlock(current, publicKey string) string {
	key := strings.TrimSpace(publicKey)
	block := insylusKeyBlockBegin + "\n" + key + "\n" + insylusKeyBlockEnd
	next, changed, _ := removeManagedAuthorizedKeyContent(current, key)
	if !changed && strings.TrimSpace(next) == key {
		next = ""
	}
	next = strings.TrimRight(next, "\n")
	if next == "" {
		return block + "\n"
	}
	return next + "\n" + block + "\n"
}

func removeManagedAuthorizedKeyContent(current, assignedKey string) (string, bool, string) {
	lines := strings.Split(current, "\n")
	var out []string
	inBlock := false
	removedBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == insylusKeyBlockBegin {
			inBlock = true
			removedBlock = true
			continue
		}
		if trimmed == insylusKeyBlockEnd && inBlock {
			inBlock = false
			continue
		}
		if inBlock {
			continue
		}
		out = append(out, line)
	}
	result := strings.TrimSpace(strings.Join(out, "\n"))
	if removedBlock {
		if result == "" {
			return "", true, ""
		}
		return result + "\n", true, ""
	}
	key := strings.TrimSpace(assignedKey)
	if key != "" && result == key {
		return "", true, ""
	}
	if key != "" && strings.Contains(result, key) {
		return current, false, "inventory-only cleanup left unmarked authorized_keys content untouched"
	}
	return current, false, ""
}
