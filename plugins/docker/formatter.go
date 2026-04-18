package docker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// FormatContainers prints a table of containers.
func FormatContainers(containers []Container) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tIMAGE\tSTATUS\tSTATE\tPORTS")
	for _, c := range containers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Image, c.Status, c.State, c.Ports)
	}
	_ = tw.Flush()
}

// FormatImages prints a table of images.
func FormatImages(images []Image) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tTAG\tID\tSIZE\tCREATED")
	for _, img := range images {
		repo := img.Repository
		if repo == "<none>" {
			repo = "-"
		}
		tag := img.Tag
		if tag == "<none>" {
			tag = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", repo, tag, img.ID, FormatBytes(img.Size), img.CreatedAt)
	}
	_ = tw.Flush()
}

// FormatDockerHosts prints configured Docker hosts.
func FormatDockerHosts(hosts []configSummary) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDOCKER HOST\tSSH USER\tTARGET ID")
	for _, host := range hosts {
		user := host.SSHUser
		if user == "" {
			user = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", host.DeviceName, host.DockerHost, user, host.DeviceID)
	}
	_ = tw.Flush()
}

// FormatContainerDetail prints detailed container info.
func FormatContainerDetail(d ContainerDetail) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID:\t%s\n", d.ID)
	fmt.Fprintf(tw, "Name:\t%s\n", d.Name)
	fmt.Fprintf(tw, "Image:\t%s\n", d.Image)
	fmt.Fprintf(tw, "State:\t%s\n", d.State)
	fmt.Fprintf(tw, "Status:\t%s\n", d.Status)
	if len(d.Networks) > 0 {
		fmt.Fprintf(tw, "Networks:\t%s\n", strings.Join(d.Networks, ", "))
	}
	if len(d.Ports) > 0 {
		var portStrs []string
		for _, p := range d.Ports {
			portStrs = append(portStrs, fmt.Sprintf("%s:%s->%s/%s", p.HostIP, p.HostPort, p.ContPort, p.Protocol))
		}
		fmt.Fprintf(tw, "Ports:\t%s\n", strings.Join(portStrs, ", "))
	}
	if len(d.Mounts) > 0 {
		fmt.Fprintf(tw, "Mounts:\t%s\n", strings.Join(d.Mounts, ", "))
	}
	if len(d.Env) > 0 {
		fmt.Fprintf(tw, "Environment:\n")
		for _, e := range d.Env {
			fmt.Fprintf(tw, "  %s\n", e)
		}
	}
	if len(d.Cmd) > 0 {
		fmt.Fprintf(tw, "Command:\t%s\n", strings.Join(d.Cmd, " "))
	}
	_ = tw.Flush()
}

// FormatLogs prints log entries.
func FormatLogs(entries []ContainerLogEntry) {
	for _, e := range entries {
		if !e.Timestamp.IsZero() {
			fmt.Fprintf(os.Stdout, "%s %s\n", e.Timestamp.Format(time.RFC3339), e.Message)
		} else {
			fmt.Fprintln(os.Stdout, e.Message)
		}
	}
}

// FormatStats prints container stats.
func FormatStats(s ContainerStats) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CPU %\tMEMORY USED\tMEMORY LIMIT\tMEMORY %")
	fmt.Fprintf(tw, "%.2f\t%s\t%s\t%.1f%%\n",
		s.CPUPercent,
		FormatBytes(s.Memory.Used),
		FormatBytes(s.Memory.Limit),
		s.Memory.Percent,
	)
	_ = tw.Flush()
}

// FormatBytes converts bytes to a human-readable string.
func FormatBytes(value uint64) string {
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
