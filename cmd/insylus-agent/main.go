package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"insylus/internal/agent"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: insylus-agent [run|install|version]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		run()
	case "install":
		install()
	case "version":
		fmt.Println(agent.Version)
	default:
		fmt.Fprintln(os.Stderr, "usage: insylus-agent [run|install|version]")
		os.Exit(2)
	}
}

func run() {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "/etc/insylus-agent/config.json", "config path")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(2)
	}
	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		panic(err)
	}
	runner := agent.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx); err != nil && err != context.Canceled {
		panic(err)
	}
}

func install() {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "", "server URL")
	bootstrapToken := fs.String("bootstrap-token", "", "device bootstrap token")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		os.Exit(2)
	}
	if *server == "" || *bootstrapToken == "" {
		fmt.Fprintln(os.Stderr, "--server and --bootstrap-token are required")
		os.Exit(2)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		os.Exit(1)
	}
	if err := agent.Install(exe, *server, *bootstrapToken); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		os.Exit(1)
	}
}
