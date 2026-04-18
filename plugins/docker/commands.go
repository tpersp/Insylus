package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/shared"
)

func command() ctl.Command {
	return ctl.Command{
		Name:  "docker",
		Usage: "docker [set-host|list-hosts|remove-host] [--host HOST] [--list|--containers|--images|--inspect NAME|--logs NAME|--stats NAME|--start NAME|--stop NAME|--restart NAME|--pause NAME|--unpause NAME] [--json] [--tail N] [--timestamps]",
		Short: "Inspect and control Docker containers",
		Long: `Interact with Docker containers on configured Docker hosts. The --host flag
accepts a Docker host config name, hostname, SSH host, or target ID. Docker
commands are executed over SSH using system SSH configuration.`,
		Examples: []string{
			"docker set-host --name docker01 --host docker01.local --ssh-user operator",
			"docker list-hosts",
			"docker --host docker01 --list",
			"docker --host docker01 --containers --json",
			"docker --host docker01 --inspect jellyfin --json",
			"docker --host docker01 --logs jellyfin --tail 50",
			"docker --host docker01 --stats jellyfin",
			"docker --host docker01 --start jellyfin",
			"docker --host docker01 --stop jellyfin",
			"docker --host docker01 --restart jellyfin",
			"docker --host docker01 --images",
			"docker --host docker01 --images --json",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "set-host":
			runSetHost(args[1:])
			return
		case "list-hosts":
			runListHosts(args[1:])
			return
		case "remove-host":
			runRemoveHost(args[1:])
			return
		}
	}
	fs := flag.NewFlagSet("docker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	hostFlag := fs.String("host", "", "Device name, hostname, IP, or ID")
	jsonOut := fs.Bool("json", false, "Print JSON output")
	list := fs.Bool("list", false, "List running containers (default)")
	containers := fs.Bool("containers", false, "List all containers (including stopped)")
	images := fs.Bool("images", false, "List Docker images")
	inspect := fs.String("inspect", "", "Inspect a container by name")
	logs := fs.String("logs", "", "Show container logs")
	stats := fs.String("stats", "", "Show container stats")
	start := fs.String("start", "", "Start a container")
	stop := fs.String("stop", "", "Stop a container")
	restart := fs.String("restart", "", "Restart a container")
	pause := fs.String("pause", "", "Pause a container")
	unpause := fs.String("unpause", "", "Unpause a container")
	tail := fs.Int("tail", 100, "Number of log lines to show")
	timestamps := fs.Bool("timestamps", false, "Include timestamps in logs")
	help := fs.Bool("help", false, "Show help")
	helpShort := fs.Bool("h", false, "Show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help || *helpShort {
		PrintHelp(os.Stdout)
		return
	}
	if *hostFlag == "" {
		api.Fatalf("--host is required")
	}
	client := api.NewClient(*serverURL)

	target, err := resolveDockerHost(client, *hostFlag)
	if err != nil {
		api.Fatalf("%v", err)
	}
	sshClient := NewDockerClient(target.DockerHost, target.SSHUser)

	actionCount := boolCount(
		*list,
		*containers,
		*images,
		*inspect != "",
		*logs != "",
		*stats != "",
		*start != "",
		*stop != "",
		*restart != "",
		*pause != "",
		*unpause != "",
	)
	if actionCount == 0 {
		*list = true
	} else if actionCount > 1 {
		api.Fatalf("choose exactly one Docker action")
	}

	switch {
	case *list:
		runListContainersSSH(sshClient, *jsonOut, false)
	case *containers:
		runListContainersSSH(sshClient, *jsonOut, true)
	case *images:
		runImagesSSH(sshClient, *jsonOut)
	case *inspect != "":
		runInspectSSH(sshClient, *inspect, *jsonOut)
	case *logs != "":
		runLogsSSH(sshClient, *logs, *tail, *timestamps, *jsonOut)
	case *stats != "":
		runStatsSSH(sshClient, *stats, *jsonOut)
	case *start != "":
		runActionSSH(sshClient, "start", *start, *jsonOut)
	case *stop != "":
		runActionSSH(sshClient, "stop", *stop, *jsonOut)
	case *restart != "":
		runActionSSH(sshClient, "restart", *restart, *jsonOut)
	case *pause != "":
		runActionSSH(sshClient, "pause", *pause, *jsonOut)
	case *unpause != "":
		runActionSSH(sshClient, "unpause", *unpause, *jsonOut)
	}
}

func runSetHost(args []string) {
	fs := flag.NewFlagSet("docker set-host", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	host := fs.String("host", "", "SSH hostname, IP, or alias for the Docker host")
	name := fs.String("name", "", "Display name for a new Docker host target")
	sshUser := fs.String("ssh-user", "", "Optional SSH user override")
	deviceID := fs.String("device-id", "", "Optional existing Insylus target ID to link")
	jsonOut := fs.Bool("json", false, "print JSON output")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help {
		PrintHelp(os.Stdout)
		return
	}
	if strings.TrimSpace(*host) == "" && strings.TrimSpace(*deviceID) == "" {
		api.Fatalf("--host or --device-id is required")
	}
	req := dockerConfig{
		DeviceID:   *deviceID,
		DeviceName: *name,
		SSHUser:    *sshUser,
		DockerHost: *host,
	}
	var out configSummary
	postJSON(api.NewClient(*serverURL), "/api/docker/config", req, &out)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Fprintf(os.Stdout, "Docker host %s saved (%s)\n", out.DeviceName, out.DockerHost)
}

func runListHosts(args []string) {
	fs := flag.NewFlagSet("docker list-hosts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help {
		PrintHelp(os.Stdout)
		return
	}
	var hosts []configSummary
	if err := api.NewClient(*serverURL).DecodeGET("/api/docker/config", &hosts); err != nil {
		api.Fatalf("%v", err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(hosts)
		return
	}
	FormatDockerHosts(hosts)
}

func runRemoveHost(args []string) {
	fs := flag.NewFlagSet("docker remove-host", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	host := fs.String("host", "", "configured Docker host name, hostname, Docker host, or target ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help {
		PrintHelp(os.Stdout)
		return
	}
	if strings.TrimSpace(*host) == "" {
		api.Fatalf("--host is required")
	}
	client := api.NewClient(*serverURL)
	target, err := resolveConfiguredDockerHost(client, *host)
	if err != nil {
		api.Fatalf("%v", err)
	}
	resp, err := client.Post("/api/docker/config/"+api.URLQueryEscape(target.DeviceID)+"/delete", "application/json", nil)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	fmt.Fprintf(os.Stdout, "Docker host %s removed\n", target.DeviceName)
}

func resolveDockerHost(client api.Client, query string) (configSummary, error) {
	if cfg, err := resolveConfiguredDockerHost(client, query); err == nil {
		return cfg, nil
	}
	device, err := resolveDevice(client, query)
	if err != nil {
		return configSummary{}, err
	}
	return configSummary{
		DeviceID:   device.Identity.ID,
		DeviceName: device.Identity.Name,
		Hostname:   device.Identity.Hostname,
		DockerHost: device.Identity.Name,
	}, nil
}

func resolveConfiguredDockerHost(client api.Client, query string) (configSummary, error) {
	var hosts []configSummary
	if err := client.DecodeGET("/api/docker/config", &hosts); err != nil {
		return configSummary{}, err
	}
	var matches []configSummary
	for _, host := range hosts {
		if strings.EqualFold(host.DeviceID, query) ||
			strings.EqualFold(host.DeviceName, query) ||
			strings.EqualFold(host.Hostname, query) ||
			strings.EqualFold(host.DockerHost, query) {
			matches = append(matches, host)
		}
	}
	if len(matches) == 0 {
		return configSummary{}, fmt.Errorf("no configured Docker host found for %q", query)
	}
	if len(matches) > 1 {
		return configSummary{}, fmt.Errorf("multiple configured Docker hosts match %q", query)
	}
	return matches[0], nil
}

func resolveDevice(client api.Client, query string) (shared.DeviceInventoryInfo, error) {
	var item shared.DeviceInventoryInfo
	if err := client.DecodeGET("/api/devices/find?q="+api.URLQueryEscape(query)+"&view=info", &item); err != nil {
		return shared.DeviceInventoryInfo{}, err
	}
	return item, nil
}

func postJSON(client api.Client, path string, request any, response any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(request); err != nil {
		api.Fatalf("encode failed: %v", err)
	}
	resp, err := client.Post(path, "application/json", &body)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		api.HandleErrorResponse(resp)
	}
	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			api.Fatalf("decode failed: %v", err)
		}
	}
}

func runListContainersSSH(client *DockerClient, jsonOut, all bool) {
	var containers []Container
	var err error
	if all {
		containers, err = client.ListAllContainers(context.Background())
	} else {
		containers, err = client.ListContainers(context.Background())
	}
	if err != nil {
		api.Fatalf("failed to list containers: %v", err)
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(containers)
		return
	}
	FormatContainers(containers)
}

func runImagesSSH(client *DockerClient, jsonOut bool) {
	images, err := client.ListImages(context.Background())
	if err != nil {
		api.Fatalf("failed to list images: %v", err)
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(images)
		return
	}
	FormatImages(images)
}

func runInspectSSH(client *DockerClient, name string, jsonOut bool) {
	detail, err := client.InspectContainer(context.Background(), name)
	if err != nil {
		api.Fatalf("failed to inspect container %q: %v", name, err)
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(detail)
		return
	}
	FormatContainerDetail(*detail)
}

func runLogsSSH(client *DockerClient, name string, tail int, timestamps, jsonOut bool) {
	logs, err := client.ContainerLogs(context.Background(), name, tail, timestamps)
	if err != nil {
		api.Fatalf("failed to get logs for %q: %v", name, err)
	}
	if jsonOut {
		entries := ParseLogOutput(logs)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(entries)
		return
	}
	// Plain text logs
	os.Stdout.Write([]byte(logs))
}

func runStatsSSH(client *DockerClient, name string, jsonOut bool) {
	stats, err := client.ContainerStats(context.Background(), name)
	if err != nil {
		api.Fatalf("failed to get stats for %q: %v", name, err)
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(stats)
		return
	}
	FormatStats(*stats)
}

func runActionSSH(client *DockerClient, action, name string, jsonOut bool) {
	var err error
	switch action {
	case "start":
		err = client.StartContainer(context.Background(), name)
	case "stop":
		err = client.StopContainer(context.Background(), name)
	case "restart":
		err = client.RestartContainer(context.Background(), name)
	case "pause":
		err = client.PauseContainer(context.Background(), name)
	case "unpause":
		err = client.UnpauseContainer(context.Background(), name)
	}
	if err != nil {
		api.Fatalf("failed to %s container %q: %v", action, name, err)
	}
	if jsonOut {
		fmt.Fprintf(os.Stdout, `{"action":"%s","container":"%s","status":"ok"}`+"\n", action, name)
	} else {
		fmt.Fprintf(os.Stdout, "%s %s: ok\n", action, name)
	}
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s docker --host HOST [--list|--containers|--images|--inspect NAME|--logs NAME|--stats NAME|--start NAME|--stop NAME|--restart NAME|--pause NAME|--unpause NAME] [--json] [--tail N] [--timestamps]\n\n", name)
	fmt.Fprintf(w, "  %s docker set-host --host HOST [--name NAME] [--ssh-user USER] [--device-id ID] [--json]\n", name)
	fmt.Fprintf(w, "  %s docker list-hosts [--json]\n", name)
	fmt.Fprintf(w, "  %s docker remove-host --host HOST [--json]\n\n", name)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --host        Configured Docker host name, hostname, Docker host, target ID, or legacy device lookup (required)\n")
	fmt.Fprintf(w, "  --server      Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --name        Display name when creating a Docker host target\n")
	fmt.Fprintf(w, "  --ssh-user    Optional SSH user override for Docker commands\n")
	fmt.Fprintf(w, "  --device-id   Link Docker config to an existing Insylus target ID\n")
	fmt.Fprintf(w, "  --list        List running containers (default action)\n")
	fmt.Fprintf(w, "  --containers  List all containers including stopped\n")
	fmt.Fprintf(w, "  --images      List Docker images\n")
	fmt.Fprintf(w, "  --inspect     Inspect a container by name\n")
	fmt.Fprintf(w, "  --logs        Show container logs\n")
	fmt.Fprintf(w, "  --stats       Show container CPU/memory stats\n")
	fmt.Fprintf(w, "  --start       Start a container\n")
	fmt.Fprintf(w, "  --stop        Stop a container\n")
	fmt.Fprintf(w, "  --restart     Restart a container\n")
	fmt.Fprintf(w, "  --pause       Pause a container\n")
	fmt.Fprintf(w, "  --unpause     Unpause a container\n")
	fmt.Fprintf(w, "  --tail N      Number of log lines (default 100)\n")
	fmt.Fprintf(w, "  --timestamps  Include timestamps in logs\n")
	fmt.Fprintf(w, "  --json        Print JSON output\n")
}

func boolCount(values ...bool) int {
	n := 0
	for _, v := range values {
		if v {
			n++
		}
	}
	return n
}
