package ctl

import (
	"fmt"
	"io"
	"os"
	"sort"
)

type Command struct {
	Name     string
	Usage    string
	Short    string
	Long     string
	Examples []string
	Help     func(io.Writer)
	Run      func(args []string)
}

type PluginInfo struct {
	ID   string
	Name string
}

type App struct {
	usage    func(io.Writer)
	commands map[string]Command
	plugins  []PluginInfo
}

func NewApp(usage func(io.Writer)) *App {
	return &App{
		usage:    usage,
		commands: map[string]Command{},
	}
}

func (a *App) Enabled() bool {
	return true
}

func (a *App) AddPlugin(info PluginInfo) {
	a.plugins = append(a.plugins, info)
}

func (a *App) AddCommand(cmd Command) {
	a.commands[cmd.Name] = cmd
}

func (a *App) Plugins() []PluginInfo {
	out := append([]PluginInfo(nil), a.plugins...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (a *App) Commands() []Command {
	out := make([]Command, 0, len(a.commands))
	for _, cmd := range a.commands {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (a *App) Command(name string) (Command, bool) {
	cmd, ok := a.commands[name]
	return cmd, ok
}

func (a *App) PrintUsage(w io.Writer) {
	if a.usage != nil {
		a.usage(w)
		return
	}
	a.defaultUsage(w)
}

func (a *App) Execute(args []string) {
	if len(args) < 2 {
		a.PrintUsage(os.Stdout)
		return
	}
	switch args[1] {
	case "--help", "-h":
		a.PrintUsage(os.Stdout)
		return
	}
	cmd, ok := a.commands[args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[1])
		a.PrintUsage(os.Stderr)
		os.Exit(2)
	}
	cmd.Run(args[2:])
}

func (a *App) defaultUsage(w io.Writer) {
	name := CommandName()
	fmt.Fprintf(w, "%s is the Insylus CLI.\n\n", name)
	fmt.Fprintf(w, "Usage:\n")
	for _, cmd := range a.Commands() {
		if cmd.Usage == "" || cmd.Name == "help" || cmd.Name == "plugins" {
			continue
		}
		fmt.Fprintf(w, "  %s %s\n", name, cmd.Usage)
	}
	fmt.Fprintf(w, "  %s plugins [PLUGIN]\n", name)
	fmt.Fprintf(w, "  %s help [COMMAND]\n", name)
	fmt.Fprintf(w, "  %s --help\n\n", name)
	fmt.Fprintf(w, "Commands:\n")
	for _, cmd := range a.Commands() {
		fmt.Fprintf(w, "  %-10s %s\n", cmd.Name, cmd.Short)
	}
}

func CommandName() string {
	if len(os.Args) == 0 {
		return "insylusctl"
	}
	return filepathBase(os.Args[0])
}

func filepathBase(path string) string {
	lastSlash := -1
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			lastSlash = i
		}
	}
	if lastSlash >= 0 && lastSlash+1 < len(path) {
		return path[lastSlash+1:]
	}
	return path
}
