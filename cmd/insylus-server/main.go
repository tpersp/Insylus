package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"insylus/internal/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sync-managed-ssh" {
		runManagedSSHSync()
		return
	}

	var cfg server.Config
	flag.StringVar(&cfg.ListenAddr, "listen", ":8080", "listen address")
	flag.StringVar(&cfg.DBPath, "db", "insylus.db", "sqlite database path")
	flag.StringVar(&cfg.PublicBaseURL, "base-url", "", "public base URL")
	flag.StringVar(&cfg.AgentBinaryPath, "agent-binary", "", "path to prebuilt insylus-agent binary")
	flag.StringVar(&cfg.ManagedUser, "managed-user", "insylus", "Linux account managed by the Access plugin")
	managedGroups := flag.String("managed-groups", "adm,systemd-journal", "comma-separated Linux groups granted in audit mode")
	flag.Parse()
	cfg.ManagedGroups = splitManagedGroups(*managedGroups)

	logger := log.New(os.Stdout, "insylus: ", log.LstdFlags|log.Lshortfile)
	app, err := server.New(cfg, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer app.Close()

	logger.Printf("listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, app.Handler()); err != nil {
		logger.Fatal(err)
	}
}

func runManagedSSHSync() {
	fs := flag.NewFlagSet("sync-managed-ssh", flag.ExitOnError)
	var opts server.ManagedSSHSyncOptions
	fs.StringVar(&opts.DBPath, "db", "/var/lib/insylus/insylus.db", "sqlite database path")
	fs.StringVar(&opts.SSHUser, "ssh-user", "insylus", "SSH user for managed aliases")
	fs.StringVar(&opts.IdentityFile, "identity-file", "/home/insylus/.ssh/id_ed25519", "SSH private key used from the controller host")
	fs.StringVar(&opts.ConfigPath, "config-path", "/etc/ssh/ssh_config.d/insylus.conf", "output SSH config path")
	fs.StringVar(&opts.KnownHostsPath, "known-hosts-path", "/etc/ssh/ssh_known_hosts_insylus", "managed known hosts path")
	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	if err := server.SyncManagedSSH(context.Background(), opts); err != nil {
		log.Fatal(err)
	}
}

func splitManagedGroups(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			groups = append(groups, part)
		}
	}
	return groups
}
