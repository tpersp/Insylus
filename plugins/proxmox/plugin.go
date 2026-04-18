package proxmox

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "proxmox"
}

func (Plugin) Name() string {
	return "Proxmox"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		rt := runtime{store: newStore(host), render: host.Web().Render}
		host.Web().NavItem(pluginhost.NavItem{Label: "Proxmox", Href: "/proxmox", Order: 45})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /proxmox", rt.handleProxmoxPage)
		host.Web().HandleFunc("POST /proxmox/tokens", rt.handleProxmoxSetToken)
		host.Web().HandleFunc("POST /proxmox/tokens/{device_id}/delete", rt.handleProxmoxDeleteToken)
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/proxmox/nodes", rt.handleProxmoxNodes)
		host.API().HandleFunc("POST /api/proxmox/tokens", rt.handleProxmoxSetToken)
		host.API().HandleFunc("POST /api/proxmox/tokens/delete/{device_id}", rt.handleProxmoxDeleteToken)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/guests", rt.handleProxmoxGuests)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/vms", rt.handleProxmoxVMs)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/lxcs", rt.handleProxmoxLXCs)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/status/{target}", rt.handleProxmoxGuestStatus)
		host.API().HandleFunc("POST /api/proxmox/{device_id}/start/{target}", rt.handleProxmoxStart)
		host.API().HandleFunc("POST /api/proxmox/{device_id}/stop/{target}", rt.handleProxmoxStop)
		host.API().HandleFunc("POST /api/proxmox/{device_id}/restart/{target}", rt.handleProxmoxRestart)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/node-status", rt.handleProxmoxNodeStatus)
		host.API().HandleFunc("GET /api/proxmox/{device_id}/cluster-status", rt.handleProxmoxClusterStatus)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "proxmox",
			Version:  1,
			Name:     "create proxmox token table",
			SQL: `
create table if not exists proxmox_tokens (
	device_id text primary key references targets(id) on delete cascade,
	node_name text not null,
	api_url text not null default '',
	token_id text not null,
	token_secret_encrypted text not null,
	role text not null default '',
	tls_insecure integer not null default 0,
	created_at text not null,
	updated_at text not null
);
create unique index if not exists proxmox_tokens_node_name_unique on proxmox_tokens(node_name collate nocase);
`,
		})
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "proxmox",
			Version:  2,
			Name:     "allow duplicate node names across clusters",
			SQL:      `drop index if exists proxmox_tokens_node_name_unique;`,
		})
	}
	return nil
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "proxmox",
		Usage: "proxmox [--server URL] --node NODE [--list|--vms|--lxcs|--info GUEST|--status GUEST|--start GUEST|--stop GUEST|--restart GUEST|--node-status|--cluster-status] [--json]",
		Short: "Query and control Proxmox VMs and LXCs",
		Long:  "Use user-provided Proxmox API tokens stored in Insylus to list guests, inspect status, and start or stop VMs and LXCs.",
		Examples: []string{
			"proxmox --node beta-pve --list",
			"proxmox --node beta-pve --info jellyfin --json",
			"proxmox --node beta-pve --start 201",
			"proxmox set-token --node beta-pve --token-id \"operator@pam!insylus\" --token-secret \"secret\"",
			"proxmox list-tokens",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}

func Run(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "set-token":
			runSetToken(args[1:])
			return
		case "list-tokens":
			runListTokens(args[1:])
			return
		case "remove-token":
			runRemoveToken(args[1:])
			return
		}
	}
	runOperation(args)
}

func runOperation(args []string) {
	fs := flag.NewFlagSet("proxmox", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	node := fs.String("node", "", "Proxmox node name, device name, hostname, IP, or ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	list := fs.Bool("list", false, "list all VMs and LXCs")
	vms := fs.Bool("vms", false, "list QEMU VMs")
	lxcs := fs.Bool("lxcs", false, "list LXC containers")
	info := fs.String("info", "", "show full guest status by name or VMID")
	status := fs.String("status", "", "show guest status by name or VMID")
	start := fs.String("start", "", "start a guest by name or VMID")
	stop := fs.String("stop", "", "stop a guest by name or VMID")
	restart := fs.String("restart", "", "restart a guest by name or VMID")
	nodeStatusFlag := fs.Bool("node-status", false, "show node CPU, memory, disk, and uptime")
	clusterStatus := fs.Bool("cluster-status", false, "show cluster-wide resource usage")
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	if *help || *helpShort {
		PrintHelp(os.Stdout)
		return
	}
	if strings.TrimSpace(*node) == "" {
		api.Fatalf("--node is required")
	}
	actionCount := boolCount(*list, *vms, *lxcs, *info != "", *status != "", *start != "", *stop != "", *restart != "", *nodeStatusFlag, *clusterStatus)
	if actionCount == 0 {
		*list = true
	} else if actionCount > 1 {
		api.Fatalf("choose exactly one Proxmox action")
	}
	client := api.NewClient(*serverURL)
	nodeInfo := resolveNode(client, *node)
	deviceID := nodeInfo.DeviceID
	switch {
	case *list:
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/guests")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out guestList
		decode(resp.Body, &out)
		printGuests(out.Guests)
	case *vms:
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/vms")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out []guest
		decode(resp.Body, &out)
		printGuests(out)
	case *lxcs:
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/lxcs")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out []guest
		decode(resp.Body, &out)
		printGuests(out)
	case *info != "" || *status != "":
		target := firstNonEmpty(*info, *status)
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/status/"+api.URLQueryEscape(target))
		defer resp.Body.Close()
		if *jsonOut || *info != "" {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out guestStatus
		decode(resp.Body, &out)
		printGuests([]guest{out.guest})
	case *start != "":
		runGuestAction(client, deviceID, "start", *start, *jsonOut)
	case *stop != "":
		runGuestAction(client, deviceID, "stop", *stop, *jsonOut)
	case *restart != "":
		runGuestAction(client, deviceID, "restart", *restart, *jsonOut)
	case *nodeStatusFlag:
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/node-status")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out nodeStatus
		decode(resp.Body, &out)
		printNodeStatus(out)
	case *clusterStatus:
		resp := mustGet(client, "/api/proxmox/"+deviceID+"/cluster-status")
		defer resp.Body.Close()
		if *jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var out []clusterResource
		decode(resp.Body, &out)
		printCluster(out)
	}
}

func runSetToken(args []string) {
	fs := flag.NewFlagSet("proxmox set-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	node := fs.String("node", "", "Insylus device or existing Proxmox node name")
	deviceID := fs.String("device-id", "", "Insylus device ID")
	nodeName := fs.String("node-name", "", "Proxmox internal node name")
	apiURL := fs.String("api-url", "", "Proxmox API base URL, default https://device:8006")
	tokenID := fs.String("token-id", "", "Proxmox token ID, e.g. user@pam!insylus")
	tokenSecret := fs.String("token-secret", "", "Proxmox token secret")
	role := fs.String("role", "", "operator note, e.g. PVEAuditor or PVEAdmin")
	tlsInsecure := fs.Bool("tls-insecure", false, "allow self-signed Proxmox certificate")
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
	if *deviceID == "" && *node == "" {
		api.Fatalf("--node or --device-id is required")
	}
	if *tokenID == "" || *tokenSecret == "" {
		api.Fatalf("--token-id and --token-secret are required")
	}
	req := map[string]any{
		"device_id":    *deviceID,
		"node":         *node,
		"node_name":    *nodeName,
		"api_url":      *apiURL,
		"token_id":     *tokenID,
		"token_secret": *tokenSecret,
		"role":         *role,
		"tls_insecure": *tlsInsecure,
	}
	resp := mustPostJSON(api.NewClient(*serverURL), "/api/proxmox/tokens", req)
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var summary tokenSummary
	decode(resp.Body, &summary)
	fmt.Fprintf(os.Stdout, "Stored Proxmox token for %s (%s)\n", summary.DeviceName, summary.NodeName)
}

func runListTokens(args []string) {
	fs := flag.NewFlagSet("proxmox list-tokens", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	jsonOut := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	resp := mustGet(api.NewClient(*serverURL), "/api/proxmox/nodes")
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var nodes []tokenSummary
	decode(resp.Body, &nodes)
	printTokenSummaries(nodes)
}

func runRemoveToken(args []string) {
	fs := flag.NewFlagSet("proxmox remove-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	node := fs.String("node", "", "Proxmox node name, device name, hostname, IP, or ID")
	deviceID := fs.String("device-id", "", "Insylus device ID")
	jsonOut := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		PrintHelp(os.Stderr)
		os.Exit(2)
	}
	client := api.NewClient(*serverURL)
	id := strings.TrimSpace(*deviceID)
	if id == "" {
		if *node == "" {
			api.Fatalf("--node or --device-id is required")
		}
		id = resolveNode(client, *node).DeviceID
	}
	resp := mustPostJSON(client, "/api/proxmox/tokens/delete/"+id, map[string]string{"device_id": id})
	defer resp.Body.Close()
	if *jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	fmt.Fprintln(os.Stdout, "Removed Proxmox token")
}

func PrintHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s proxmox --node NODE [--list|--vms|--lxcs|--info GUEST|--status GUEST|--start GUEST|--stop GUEST|--restart GUEST|--node-status|--cluster-status] [--json]\n", name)
	fmt.Fprintf(w, "  %s proxmox set-token --node DEVICE --token-id ID --token-secret SECRET [--node-name NODE] [--api-url URL] [--tls-insecure]\n", name)
	fmt.Fprintf(w, "  %s proxmox list-tokens [--json]\n", name)
	fmt.Fprintf(w, "  %s proxmox remove-token --node NODE\n\n", name)
	fmt.Fprintf(w, "Token setup:\n")
	fmt.Fprintf(w, "  Create the API token in Proxmox first. Insylus never creates Proxmox API tokens.\n")
	fmt.Fprintf(w, "  Suggested read-only role: PVEAuditor. Start/stop/restart needs VM.PowerMgmt on the target path, commonly via PVEAdmin.\n\n")
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --server        Insylus server URL, default INSYLUS_SERVER_URL or http://127.0.0.1:8080\n")
	fmt.Fprintf(w, "  --node          Proxmox node name, device name, hostname, IP, or ID\n")
	fmt.Fprintf(w, "  --json          Print JSON output\n")
}

func resolveNode(client api.Client, query string) tokenSummary {
	var nodes []tokenSummary
	if err := client.DecodeGET("/api/proxmox/nodes", &nodes); err != nil {
		api.Fatalf("%v", err)
	}
	var matches []tokenSummary
	for _, node := range nodes {
		if strings.EqualFold(node.DeviceID, query) ||
			strings.EqualFold(node.DeviceName, query) ||
			strings.EqualFold(node.Hostname, query) ||
			strings.EqualFold(node.NodeName, query) {
			matches = append(matches, node)
		}
	}
	if len(matches) == 0 {
		api.Fatalf("no Proxmox node named %q is enrolled or configured", query)
	}
	if len(matches) > 1 {
		api.Fatalf("multiple Proxmox nodes match %q; use device ID", query)
	}
	if !matches[0].HasToken {
		api.Fatalf("no Proxmox API token configured for %s\nHint: create a token in Proxmox, then run: %s proxmox set-token --node %s --token-id \"user@pam!insylus\" --token-secret \"your-secret\"", matches[0].DeviceName, ctl.CommandName(), query)
	}
	return matches[0]
}

func runGuestAction(client api.Client, deviceID, action, target string, jsonOut bool) {
	resp := mustPostJSON(client, "/api/proxmox/"+deviceID+"/"+action+"/"+api.URLQueryEscape(target), map[string]string{})
	defer resp.Body.Close()
	if jsonOut {
		api.PrintRawJSON(resp.Body)
		return
	}
	var result actionResult
	decode(resp.Body, &result)
	fmt.Fprintf(os.Stdout, "%s submitted for %s %d on %s\n", result.Action, result.Type, result.VMID, result.Node)
}

func mustGet(client api.Client, path string) *http.Response {
	resp, err := client.Get(path)
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}
	return resp
}

func mustPostJSON(client api.Client, path string, body any) *http.Response {
	data, _ := json.Marshal(body)
	resp, err := client.Post(path, "application/json", bytes.NewReader(data))
	if err != nil {
		api.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode >= 300 {
		api.HandleErrorResponse(resp)
	}
	return resp
}

func decode(r io.Reader, dst any) {
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		api.Fatalf("decode failed: %v", err)
	}
}

func printGuests(items []guest) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "VMID\tNAME\tTYPE\tSTATUS\tCPU\tMEMORY\tDISK\tUPTIME")
	for _, item := range items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%.2f\t%s / %s\t%s / %s\t%s\n",
			item.VMID, item.Name, item.Type, item.Status, item.CPU,
			formatBytes(item.Memory), formatBytes(item.MaxMemory),
			formatBytes(item.DiskUsed), formatBytes(item.DiskTotal),
			formatDuration(item.Uptime))
	}
	_ = tw.Flush()
}

func printTokenSummaries(items []tokenSummary) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DEVICE\tNODE\tTOKEN\tROLE\tAPI URL\tTLS")
	for _, item := range items {
		token := "missing"
		if item.HasToken {
			token = item.TokenID
		}
		tlsMode := "verify"
		if item.TLSInsecure {
			tlsMode = "insecure"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", item.DeviceName, item.NodeName, token, item.Role, item.APIURL, tlsMode)
	}
	_ = tw.Flush()
}

func printNodeStatus(item nodeStatus) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NODE\tCPU\tMEMORY\tDISK\tUPTIME")
	fmt.Fprintf(tw, "%s\t%.2f\t%s / %s\t%s / %s\t%s\n",
		item.Node, item.CPU, formatBytes(item.MemoryUsed), formatBytes(item.MemoryTotal),
		formatBytes(item.DiskUsed), formatBytes(item.DiskTotal), formatDuration(item.Uptime))
	_ = tw.Flush()
}

func printCluster(items []clusterResource) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tID\tNODE\tNAME\tSTATUS\tCPU\tMEMORY\tDISK\tUPTIME")
	for _, item := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.2f\t%s / %s\t%s / %s\t%s\n",
			item.Type, item.ID, item.Node, item.Name, item.Status, item.CPU,
			formatBytes(item.Memory), formatBytes(item.MaxMemory),
			formatBytes(item.Disk), formatBytes(item.MaxDisk), formatDuration(item.Uptime))
	}
	_ = tw.Flush()
}

func formatBytes(value uint64) string {
	if value == 0 {
		return "-"
	}
	const unit = 1024
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f := float64(value)
	i := 0
	for f >= unit && i < len(units)-1 {
		f /= unit
		i++
	}
	if i == 0 {
		return strconv.FormatUint(value, 10) + " B"
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func boolCount(values ...bool) int {
	n := 0
	for _, value := range values {
		if value {
			n++
		}
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
