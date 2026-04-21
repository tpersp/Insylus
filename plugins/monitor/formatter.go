package monitor

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"insylus/internal/api"
	"insylus/internal/ctl"
)

func command() ctl.Command {
	return ctl.Command{
		Name:  "monitor",
		Usage: "monitor [--server URL] [--json] [--list | --status TARGET | --history TARGET] [--window 30m|1h|24h]",
		Short: "Show network monitor status and history",
		Long:  "List active monitor targets, inspect one target, or fetch recent latency and availability history.",
		Examples: []string{
			"monitor",
			"monitor --status MiscServer",
			"monitor --history 10.10.10.22 --window 1h",
			"monitor --json --list",
		},
		Help: printHelp,
		Run:  run,
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	listFlag := fs.Bool("list", false, "list monitor targets")
	statusQuery := fs.String("status", "", "show one target by device ID, name, host, or key")
	historyQuery := fs.String("history", "", "show recent history for one target by device ID, name, host, or key")
	window := fs.String("window", "1h", "history window: 30m, 1h, or 24h")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	if err := fs.Parse(args); err != nil {
		printHelp(os.Stderr)
		os.Exit(2)
	}
	if *help || *helpShort {
		printHelp(os.Stdout)
		return
	}
	if *statusQuery != "" && *historyQuery != "" {
		api.Fatalf("--status and --history are mutually exclusive")
	}

	client := api.NewClient(*serverURL)
	var statuses []Status
	if err := client.DecodeGET("/api/monitor", &statuses); err != nil {
		api.Fatalf("%v", err)
	}

	switch {
	case *historyQuery != "":
		status := mustResolveStatus(statuses, *historyQuery)
		var response HistoryResponse
		if err := client.DecodeGET("/api/monitor/"+api.URLQueryEscape(status.Key)+"/history?window="+api.URLQueryEscape(*window), &response); err != nil {
			api.Fatalf("%v", err)
		}
		if *jsonOut {
			raw, _ := json.MarshalIndent(response, "", "  ")
			fmt.Fprintln(os.Stdout, string(raw))
			return
		}
		printHistory(response)
	case *statusQuery != "":
		status := mustResolveStatus(statuses, *statusQuery)
		if *jsonOut {
			raw, _ := json.MarshalIndent(status, "", "  ")
			fmt.Fprintln(os.Stdout, string(raw))
			return
		}
		printStatus(status)
	default:
		_ = listFlag
		if *jsonOut {
			raw, _ := json.MarshalIndent(statuses, "", "  ")
			fmt.Fprintln(os.Stdout, string(raw))
			return
		}
		printStatuses(statuses)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s monitor [--server URL] [--json] [--list | --status TARGET | --history TARGET] [--window 30m|1h|24h]\n", ctl.CommandName())
	fmt.Fprintf(w, "\nExamples:\n")
	fmt.Fprintf(w, "  %s monitor\n", ctl.CommandName())
	fmt.Fprintf(w, "  %s monitor --status MiscServer\n", ctl.CommandName())
	fmt.Fprintf(w, "  %s monitor --history 10.10.10.22 --window 1h\n", ctl.CommandName())
}

func mustResolveStatus(statuses []Status, query string) Status {
	query = strings.TrimSpace(query)
	var matches []Status
	for _, status := range statuses {
		if strings.EqualFold(status.Key, query) ||
			strings.EqualFold(status.DeviceID, query) ||
			strings.EqualFold(status.Name, query) ||
			strings.EqualFold(status.Host, query) {
			matches = append(matches, status)
		}
	}
	if len(matches) == 0 {
		api.Fatalf("monitor target not found: %s", query)
	}
	if len(matches) > 1 {
		var choices []string
		for _, status := range matches {
			choices = append(choices, fmt.Sprintf("%s (%s)", status.Name, status.Key))
		}
		sort.Strings(choices)
		api.Fatalf("monitor query matched multiple targets: %s", strings.Join(choices, ", "))
	}
	return matches[0]
}

func printStatuses(statuses []Status) {
	if len(statuses) == 0 {
		fmt.Fprintln(os.Stdout, "No monitor targets yet.")
		return
	}
	for _, status := range statuses {
		latency := "-"
		if status.LatencyMs > 0 {
			latency = fmt.Sprintf("%.1fms", status.LatencyMs)
		}
		state := status.State
		if state == "" {
			state = "unknown"
		}
		fmt.Fprintf(
			os.Stdout,
			"%-20s %-8s %-18s %-6s avail %5.1f%% latency %8s checked %s\n",
			status.Name,
			state,
			status.Host,
			strings.ToUpper(status.MonitorMethod),
			status.Availability24h,
			latency,
			formatOptionalTime(status.LastCheckedAt),
		)
	}
}

func printStatus(status Status) {
	fmt.Fprintf(os.Stdout, "Name:           %s\n", status.Name)
	fmt.Fprintf(os.Stdout, "Source:         %s\n", status.Source)
	fmt.Fprintf(os.Stdout, "Key:            %s\n", status.Key)
	fmt.Fprintf(os.Stdout, "Host:           %s\n", status.Host)
	if status.Port > 0 {
		fmt.Fprintf(os.Stdout, "Port:           %d\n", status.Port)
	}
	fmt.Fprintf(os.Stdout, "Method:         %s\n", strings.ToUpper(status.MonitorMethod))
	fmt.Fprintf(os.Stdout, "State:          %s\n", status.State)
	fmt.Fprintf(os.Stdout, "Availability:   %.1f%% (24h)\n", status.Availability24h)
	if status.LatencyMs > 0 {
		fmt.Fprintf(os.Stdout, "Latency:        %.1fms\n", status.LatencyMs)
	}
	fmt.Fprintf(os.Stdout, "Last checked:   %s\n", formatOptionalTime(status.LastCheckedAt))
	if status.LastError != "" {
		fmt.Fprintf(os.Stdout, "Last error:     %s\n", status.LastError)
	}
}

func printHistory(response HistoryResponse) {
	printStatus(response.Target)
	fmt.Fprintf(os.Stdout, "\nHistory (%s):\n", response.Window)
	if len(response.Points) == 0 {
		fmt.Fprintln(os.Stdout, "  No samples in this window.")
		return
	}
	for _, point := range response.Points {
		state := "down"
		if point.Success {
			state = "up"
		}
		latency := "-"
		if point.LatencyMs > 0 {
			latency = fmt.Sprintf("%.1fms", point.LatencyMs)
		}
		fmt.Fprintf(os.Stdout, "  %s %-4s %8s", point.CheckedAt.Local().Format("2006-01-02 15:04:05"), state, latency)
		if point.Error != "" {
			fmt.Fprintf(os.Stdout, "  %s", point.Error)
		}
		fmt.Fprintln(os.Stdout)
	}
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
