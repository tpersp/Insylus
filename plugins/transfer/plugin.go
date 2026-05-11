package transfer

import (
	"insylus/internal/pluginhost"
)

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "transfer"
}

func (Plugin) Name() string {
	return "Transfer"
}

func (Plugin) Manifest() pluginhost.PluginManifest {
	return pluginhost.PluginManifest{
		ID:       "transfer",
		Name:     "Transfer",
		Version:  "dev",
		Provides: []string{"plugin.transfer", "cli.transfer"},
		Requires: []string{"devices", "access"},
		CLI:      true,
	}
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	return nil
}
