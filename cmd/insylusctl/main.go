package main

import (
	"os"

	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
	"insylus/plugins/registry"
)

func main() {
	app := ctl.NewApp(nil)
	host := pluginhost.NewCLIOnlyHost(app)
	for _, plugin := range registry.Plugins() {
		before := len(app.Commands())
		if err := plugin.Register(host); err != nil {
			panic(err)
		}
		if len(app.Commands()) > before {
			app.AddPlugin(ctl.PluginInfo{ID: plugin.ID(), Name: plugin.Name()})
		}
	}
	app.Execute(os.Args)
}
