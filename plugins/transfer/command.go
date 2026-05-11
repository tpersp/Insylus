package transfer

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"insylus/internal/api"
	"insylus/internal/ctl"
	"insylus/internal/finder"
	"insylus/internal/shared"
)

type endpoint struct {
	Raw        string
	Device     string
	Path       string
	Remote     bool
	Resolved   *shared.DeviceInventoryInfo
	ResolvedAs string
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty SSH option")
	}
	*s = append(*s, value)
	return nil
}

func command() ctl.Command {
	return ctl.Command{
		Name:  "transfer",
		Usage: "transfer [--server URL] [--dry-run] [-r] [-p] SOURCE DEST",
		Short: "Broker file copies between devices through the controller",
		Long:  "Copy files with scp -3 from the controller host, resolving DEVICE:/path endpoints through Insylus managed SSH aliases.",
		Examples: []string{
			"transfer docker01:/srv/media/movie.mkv animus:/srv/media/",
			"transfer -r docker01:/srv/photos animus:/srv/archive/",
			"transfer --dry-run docker01:/tmp/report.txt animus:/tmp/report.txt",
		},
		Help: printHelp,
		Run:  run,
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("transfer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverURL := fs.String("server", api.DefaultServerURL(), "Insylus server URL")
	dryRun := fs.Bool("dry-run", false, "print the resolved scp command without running it")
	recursive := fs.Bool("r", false, "copy directories recursively")
	preserve := fs.Bool("p", false, "preserve modification times and modes")
	verbose := fs.Bool("v", false, "enable scp verbose output")
	scpPath := fs.String("scp", "scp", "scp binary path")
	var sshOptions stringList
	fs.Var(&sshOptions, "ssh-option", "additional SSH option passed to scp as -o OPTION; may be repeated")
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
	rest := fs.Args()
	if len(rest) != 2 {
		printHelp(os.Stderr)
		os.Exit(2)
	}

	client := api.NewClient(*serverURL)
	src := resolveEndpoint(client, rest[0])
	dst := resolveEndpoint(client, rest[1])
	if !src.Remote && !dst.Remote {
		api.Fatalf("at least one endpoint must be DEVICE:/path")
	}

	cmdArgs := scpArgs(src, dst, scpOptions{
		Recursive:  *recursive,
		Preserve:   *preserve,
		Verbose:    *verbose,
		SSHOptions: append([]string(nil), sshOptions...),
	})
	if *dryRun {
		fmt.Fprintln(os.Stdout, shellCommand(append([]string{*scpPath}, cmdArgs...)))
		return
	}
	cmd := exec.Command(*scpPath, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		api.Fatalf("transfer failed: %v", err)
	}
}

type scpOptions struct {
	Recursive  bool
	Preserve   bool
	Verbose    bool
	SSHOptions []string
}

func scpArgs(src, dst endpoint, opts scpOptions) []string {
	args := []string{"-3"}
	if opts.Recursive {
		args = append(args, "-r")
	}
	if opts.Preserve {
		args = append(args, "-p")
	}
	if opts.Verbose {
		args = append(args, "-v")
	}
	for _, option := range opts.SSHOptions {
		args = append(args, "-o", option)
	}
	args = append(args, src.SCPArg(), dst.SCPArg())
	return args
}

func resolveEndpoint(client api.Client, raw string) endpoint {
	ep := parseEndpoint(raw)
	if !ep.Remote {
		return ep
	}
	device, err := finder.FindDevice(client, ep.Device)
	if err != nil {
		api.Fatalf("resolve %q: %v", ep.Device, err)
	}
	if err := requireSSHReady(*device); err != nil {
		api.Fatalf("%s is not ready for managed SSH: %v", device.Identity.Name, err)
	}
	ep.Resolved = device
	ep.ResolvedAs = device.Connection.SSHAlias
	return ep
}

func parseEndpoint(raw string) endpoint {
	raw = strings.TrimSpace(raw)
	host, path, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(host) == "" {
		return endpoint{Raw: raw, Path: raw}
	}
	return endpoint{
		Raw:    raw,
		Device: strings.TrimSpace(host),
		Path:   path,
		Remote: true,
	}
}

func (e endpoint) SCPArg() string {
	if !e.Remote {
		return e.Path
	}
	alias := e.ResolvedAs
	if alias == "" {
		alias = e.Device
	}
	return alias + ":" + e.Path
}

func requireSSHReady(device shared.DeviceInventoryInfo) error {
	switch {
	case device.Access.DeviceMode != shared.DeviceModeAccessManaged:
		return fmt.Errorf("device_mode is %q", device.Access.DeviceMode)
	case !device.Access.ManagedAccountEnabled:
		return fmt.Errorf("managed_account_enabled is false")
	case device.Access.AccessMode == shared.AccessModeDisabled:
		return fmt.Errorf("access_mode is disabled")
	case !device.Access.EnforcementSucceeded:
		if strings.TrimSpace(device.Access.ErrorMessage) != "" {
			return fmt.Errorf("last enforcement failed: %s", device.Access.ErrorMessage)
		}
		return fmt.Errorf("last enforcement has not succeeded")
	case strings.TrimSpace(device.Connection.SSHAlias) == "":
		return fmt.Errorf("ssh_alias is empty")
	default:
		return nil
	}
}

func shellCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' || r == ',' || r == '@' || r == '+' || r == '%' || r == '~' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}

func printHelp(w io.Writer) {
	name := ctl.CommandName()
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s transfer [--server URL] [--dry-run] [-r] [-p] SOURCE DEST\n", name)
	fmt.Fprintf(w, "\nEndpoint syntax:\n")
	fmt.Fprintf(w, "  DEVICE:/path     Remote path on an Insylus managed SSH device\n")
	fmt.Fprintf(w, "  /local/path      Local path on the controller host\n")
	fmt.Fprintf(w, "\nOptions:\n")
	fmt.Fprintf(w, "  --dry-run        Print the resolved scp -3 command without running it\n")
	fmt.Fprintf(w, "  -r               Copy directories recursively\n")
	fmt.Fprintf(w, "  -p               Preserve modification times and modes\n")
	fmt.Fprintf(w, "  -v               Enable scp verbose output\n")
	fmt.Fprintf(w, "  --ssh-option O   Pass an SSH option to scp as -o O; may be repeated\n")
	fmt.Fprintf(w, "\nExamples:\n")
	fmt.Fprintf(w, "  %s transfer docker01:/srv/media/movie.mkv animus:/srv/media/\n", name)
	fmt.Fprintf(w, "  %s transfer -r docker01:/srv/photos animus:/srv/archive/\n", name)
	fmt.Fprintf(w, "  %s transfer --dry-run docker01:/tmp/report.txt animus:/tmp/report.txt\n", name)
}
