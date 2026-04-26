package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"insylus/internal/server"
	"insylus/internal/version"
)

const (
	serverReadHeaderTimeout = 5 * time.Second
	serverIdleTimeout       = 60 * time.Second
	serverShutdownTimeout   = 10 * time.Second
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.ServerVersion)
		return
	}

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
	defer func() {
		if err := app.Close(); err != nil {
			logger.Printf("close app: %v", err)
		}
	}()

	if err := serve(cfg.ListenAddr, app.Handler(), logger); err != nil {
		logger.Fatal(err)
	}
}

func serve(addr string, handler http.Handler, logger *log.Logger) error {
	srv := newHTTPServer(addr, handler)
	errCh := make(chan error, 1)
	go func() {
		if logger != nil {
			logger.Printf("listening on %s", addr)
		}
		errCh <- srv.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopCh)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-stopCh:
		if logger != nil {
			logger.Printf("received %s, shutting down", sig)
		}
		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
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
