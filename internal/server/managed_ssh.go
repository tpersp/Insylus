package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"insylus/internal/shared"
)

type ManagedSSHSyncOptions struct {
	DBPath         string
	SSHUser        string
	IdentityFile   string
	ConfigPath     string
	KnownHostsPath string
}

func SyncManagedSSH(ctx context.Context, opts ManagedSSHSyncOptions) error {
	store, err := OpenStore(opts.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	opts, err = resolveManagedSSHOptions(ctx, store, opts)
	if err != nil {
		return err
	}

	records, err := store.ListDevices(ctx)
	if err != nil {
		return err
	}

	configContent := renderManagedSSHConfig(records, opts.SSHUser, opts.IdentityFile, opts.KnownHostsPath)
	if err := os.MkdirAll(filepath.Dir(opts.ConfigPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(opts.ConfigPath, []byte(configContent), 0o644); err != nil {
		return err
	}

	knownHosts, err := collectKnownHosts(ctx, records)
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.KnownHostsPath, []byte(knownHosts), 0o644); err != nil {
		return err
	}
	return nil
}

func resolveManagedSSHOptions(ctx context.Context, store *Store, opts ManagedSSHSyncOptions) (ManagedSSHSyncOptions, error) {
	previousSSHUser := strings.TrimSpace(opts.SSHUser)
	cfg, err := store.ManagedAccountConfig(ctx, shared.ManagedAccountConfig{
		ManagedUser: previousSSHUser,
		AccessMode:  shared.AccessModeAudit,
	})
	if err != nil {
		return opts, err
	}
	managedUser := strings.TrimSpace(cfg.ManagedUser)
	if managedUser == "" {
		managedUser = shared.DefaultManagedUser
	}
	opts.SSHUser = managedUser

	identityFile := strings.TrimSpace(opts.IdentityFile)
	if identityFile == "" || (previousSSHUser != "" && previousSSHUser != managedUser && identityFile == defaultManagedSSHIdentityFile(previousSSHUser)) {
		identityFile = defaultManagedSSHIdentityFile(managedUser)
	}
	opts.IdentityFile = identityFile
	return opts, nil
}

func defaultManagedSSHIdentityFile(user string) string {
	user = strings.TrimSpace(user)
	if user == "" {
		user = shared.DefaultManagedUser
	}
	return "/home/" + user + "/.ssh/id_ed25519"
}

func renderManagedSSHConfig(records []DeviceRecord, sshUser, identityFile, knownHostsPath string) string {
	var b strings.Builder
	b.WriteString("# Managed by Insylus. Do not edit manually.\n")
	for _, record := range records {
		if record.Policy.DeviceMode != "access-managed" || !record.Policy.ManagedAccountEnabled || record.Policy.AccessMode == "disabled" || len(record.Device.IPs) == 0 {
			continue
		}
		aliases := deviceAliases(record.Device.Name)
		b.WriteString("\n")
		b.WriteString("Host ")
		b.WriteString(strings.Join(aliases, " "))
		b.WriteString("\n")
		b.WriteString("    HostName ")
		b.WriteString(record.Device.IPs[0])
		b.WriteString("\n")
		b.WriteString("    User ")
		b.WriteString(sshUser)
		b.WriteString("\n")
		b.WriteString("    IdentityFile ")
		b.WriteString(identityFile)
		b.WriteString("\n")
		b.WriteString("    IdentitiesOnly yes\n")
		b.WriteString("    StrictHostKeyChecking accept-new\n")
		b.WriteString("    GlobalKnownHostsFile ")
		b.WriteString(knownHostsPath)
		b.WriteString(" /etc/ssh/ssh_known_hosts\n")
	}
	return b.String()
}

func collectKnownHosts(ctx context.Context, records []DeviceRecord) (string, error) {
	seen := map[string]struct{}{}
	var hosts []string
	for _, record := range records {
		if record.Policy.DeviceMode != "access-managed" || !record.Policy.ManagedAccountEnabled || record.Policy.AccessMode == "disabled" || len(record.Device.IPs) == 0 {
			continue
		}
		ip := record.Device.IPs[0]
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		hosts = append(hosts, ip)
	}
	sort.Strings(hosts)
	var out strings.Builder
	out.WriteString("# Managed by Insylus. Do not edit manually.\n")
	for _, host := range hosts {
		scanned, err := exec.CommandContext(ctx, "ssh-keyscan", "-H", host).CombinedOutput()
		if err != nil {
			out.WriteString(fmt.Sprintf("# ssh-keyscan failed for %s: %s\n", host, strings.TrimSpace(string(scanned))))
			continue
		}
		out.Write(scanned)
	}
	return out.String(), nil
}

func deviceAliases(name string) []string {
	base := strings.TrimSpace(name)
	lower := strings.ToLower(base)
	if base == "" {
		return nil
	}
	if base == lower {
		return []string{base}
	}
	return []string{base, lower}
}

func managedPrimaryAlias(name string) string {
	aliases := deviceAliases(name)
	if len(aliases) == 0 {
		return ""
	}
	return aliases[len(aliases)-1]
}
