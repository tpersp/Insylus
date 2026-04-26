package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"insylus/internal/agent"
)

func main() {
	os.Exit(runMain(os.Args, os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		printUsage(stderr)
		return 2
	}
	switch args[1] {
	case "run":
		return runAgent(args[2:], stderr)
	case "install":
		return installAgent(args[2:], stderr)
	case "version":
		fmt.Fprintln(stdout, agent.Version)
		return 0
	default:
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: insylus-agent [run|install|version]")
}

func runAgent(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "/etc/insylus-agent/config.json", "config path")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "run failed: %v\n", err)
		return 2
	}
	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "run failed: %v\n", err)
		return 1
	}
	runner := agent.New(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(stderr, "run failed: %v\n", err)
		return 1
	}
	return 0
}

func installAgent(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "server URL")
	bootstrapToken := fs.String("bootstrap-token", "", "device bootstrap token")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "install failed: %v\n", err)
		return 2
	}
	if *server == "" || *bootstrapToken == "" {
		fmt.Fprintln(stderr, "--server and --bootstrap-token are required")
		return 2
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "install failed: %v\n", err)
		return 1
	}
	if err := agent.Install(exe, *server, *bootstrapToken); err != nil {
		fmt.Fprintf(stderr, "install failed: %v\n", err)
		return 1
	}
	return 0
}
