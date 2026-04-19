package help

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
)

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "help"
}

func (Plugin) Name() string {
	return "Help"
}

func (Plugin) Register(host pluginhost.Host) error {
	if !host.CLI().Enabled() {
		return nil
	}
	app := host.CLI()
	app.AddCommand(ctl.Command{
		Name:  "help",
		Usage: "help [COMMAND]",
		Short: "Show Insylus CLI help",
		Long:  "Show general CLI help or detailed help for one registered plugin command.",
		Examples: []string{
			"help",
			"help devices",
			"help services",
		},
		Run: func(args []string) {
			runHelp(app, args)
		},
	})
	app.AddCommand(ctl.Command{
		Name:  "plugins",
		Usage: "plugins [list|enable|disable|purge|profiles|apply-profile] [ARGS]",
		Short: "Manage runtime plugins",
		Long:  "List, enable, disable, purge, and apply runtime plugin profiles through the Insylus server.",
		Examples: []string{
			"plugins list",
			"plugins enable docker",
			"plugins apply-profile homelab",
		},
		Run: func(args []string) {
			runPlugins(app, args)
		},
	})
	return nil
}

func runHelp(app pluginhost.CLIHost, args []string) {
	if len(args) == 0 {
		app.PrintUsage(os.Stdout)
		return
	}
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: help [COMMAND]")
		os.Exit(2)
	}
	cmd, ok := app.Command(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		os.Exit(2)
	}
	if cmd.Help != nil {
		cmd.Help(os.Stdout)
		return
	}
	printCommandDetails(os.Stdout, cmd)
}

func runPlugins(app pluginhost.CLIHost, args []string) {
	serverURL, jsonOut, rest := parsePluginArgs(args)
	action := "list"
	if len(rest) > 0 {
		action = rest[0]
		rest = rest[1:]
	}
	client := api.NewClient(serverURL)
	switch action {
	case "list":
		resp := mustGet(client, "/api/plugins")
		defer resp.Body.Close()
		if jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var plugins []pluginhost.PluginManifest
		decode(resp.Body, &plugins)
		printPluginList(plugins)
	case "profiles":
		resp := mustGet(client, "/api/plugins/profiles")
		defer resp.Body.Close()
		if jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var profiles []pluginhost.PluginProfile
		decode(resp.Body, &profiles)
		for _, profile := range profiles {
			fmt.Fprintf(os.Stdout, "%-16s %s\n", profile.Name, strings.Join(profile.PluginIDs, ", "))
		}
	case "enable", "disable", "purge":
		if len(rest) != 1 {
			fmt.Fprintf(os.Stderr, "usage: plugins %s PLUGIN\n", action)
			os.Exit(2)
		}
		resp := mustPost(client, "/api/plugins/"+rest[0]+"/"+action)
		defer resp.Body.Close()
		if jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var result map[string]string
		decode(resp.Body, &result)
		fmt.Fprintf(os.Stdout, "%s %s\n", action, rest[0])
		printRestartHint(result)
	case "apply-profile":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: plugins apply-profile PROFILE")
			os.Exit(2)
		}
		resp := mustPost(client, "/api/plugins/profiles/"+rest[0]+"/apply")
		defer resp.Body.Close()
		if jsonOut {
			api.PrintRawJSON(resp.Body)
			return
		}
		var result map[string]string
		decode(resp.Body, &result)
		fmt.Fprintf(os.Stdout, "applied profile %s\n", rest[0])
		printRestartHint(result)
	default:
		cmd, ok := app.Command(action)
		if ok {
			printCommandDetails(os.Stdout, cmd)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown plugin action: %s\n", action)
		os.Exit(2)
	}
}

func printRestartHint(result map[string]string) {
	switch result["restart"] {
	case "required":
		fmt.Fprintln(os.Stdout, "restart insylus.service for newly enabled plugin routes and nav links to appear")
	case "not_required":
		fmt.Fprintln(os.Stdout, "disabled plugin routes are gated immediately")
	}
}

func parsePluginArgs(args []string) (string, bool, []string) {
	serverURL := api.DefaultServerURL()
	jsonOut := false
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--server":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "usage: plugins [list|enable|disable|purge|profiles|apply-profile] [--server URL] [--json] [ARGS]")
				os.Exit(2)
			}
			i++
			serverURL = args[i]
		case "--help", "-h":
			fmt.Fprintln(os.Stdout, "usage: plugins [list|enable|disable|purge|profiles|apply-profile] [--server URL] [--json] [ARGS]")
			os.Exit(0)
		default:
			rest = append(rest, args[i])
		}
	}
	return serverURL, jsonOut, rest
}

func printPluginList(plugins []pluginhost.PluginManifest) {
	for _, plugin := range plugins {
		state := "disabled"
		if plugin.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(os.Stdout, "%-12s %-8s %s\n", plugin.ID, state, plugin.Name)
	}
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

func mustPost(client api.Client, path string) *http.Response {
	resp, err := client.Post(path, "application/json", nil)
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

func printCommandDetails(w io.Writer, cmd ctl.Command) {
	fmt.Fprintf(w, "%s\n\n", cmd.Name)
	if cmd.Long != "" {
		fmt.Fprintf(w, "%s\n\n", cmd.Long)
	} else if cmd.Short != "" {
		fmt.Fprintf(w, "%s\n\n", cmd.Short)
	}
	if cmd.Usage != "" {
		fmt.Fprintf(w, "Usage:\n")
		fmt.Fprintf(w, "  %s %s\n", ctl.CommandName(), cmd.Usage)
	}
	if len(cmd.Examples) > 0 {
		fmt.Fprintf(w, "\nExamples:\n")
		for _, example := range cmd.Examples {
			fmt.Fprintf(w, "  %s %s\n", ctl.CommandName(), example)
		}
	}
}
