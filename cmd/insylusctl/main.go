package main

import (
	"fmt"
	"io"
	"os"

	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
	"insylus/plugins/registry"
)

func main() {
	os.Exit(runMain(os.Args, os.Stderr))
}

func runMain(args []string, stderr io.Writer) int {
	app := ctl.NewApp(nil)
	host := pluginhost.NewCLIOnlyHost(app)
	for _, plugin := range registry.Plugins() {
		before := len(app.Commands())
		if err := plugin.Register(host); err != nil {
			fmt.Fprintf(stderr, "plugin %s registration failed: %v\n", plugin.ID(), err)
			return 1
		}
		if len(app.Commands()) > before {
			app.AddPlugin(ctl.PluginInfo{ID: plugin.ID(), Name: plugin.Name()})
		}
	}
	app.Execute(args)
	return 0
}
