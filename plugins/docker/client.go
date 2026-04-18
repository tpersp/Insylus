package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DockerClient runs docker commands over SSH on a remote host.
type DockerClient struct {
	host    string
	sshUser string
}

// NewDockerClient creates a DockerClient for the given host (device name, hostname, IP, or alias).
// The sshUser parameter optionally overrides the default SSH user.
func NewDockerClient(host, sshUser string) *DockerClient {
	return &DockerClient{host: host, sshUser: sshUser}
}

// ssher runs an SSH command and returns the combined output.
// The remote command is passed as a single string so the remote shell
// properly handles quoting for arguments like Go templates with {{}}.
func (c *DockerClient) ssher(ctx context.Context, args ...string) (string, error) {
	// Build the remote command as a single string for proper shell quoting
	var cmdStr strings.Builder
	for i, arg := range args {
		if i > 0 {
			cmdStr.WriteByte(' ')
		}
		// Quote the argument if it contains spaces, braces, or other special chars
		if strings.ContainsAny(arg, " {}\"'$`\\") {
			// Use single quotes and escape any embedded single quotes
			singleQuoted := strings.ReplaceAll(arg, "'", "'\\''")
			cmdStr.WriteString("'")
			cmdStr.WriteString(singleQuoted)
			cmdStr.WriteByte('\'')
		} else {
			cmdStr.WriteString(arg)
		}
	}

	// Build SSH arguments
	sshArgs := []string{"ssh", "-o", "LogVerbose=0", "-o", "StrictHostKeyChecking=no"}
	if c.sshUser != "" {
		sshArgs = append(sshArgs, "-l", c.sshUser)
	}
	sshArgs = append(sshArgs, c.host, cmdStr.String())

	cmd := exec.CommandContext(ctx, sshArgs[0], sshArgs[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("ssh: %s: %s", strings.TrimSpace(stderr.String()), err)
		}
		return "", fmt.Errorf("ssh: %s", err)
	}
	return string(out), nil
}

// Ping checks whether the remote host has Docker CLI available.
func (c *DockerClient) Ping(ctx context.Context) error {
	_, err := c.ssher(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return fmt.Errorf("docker not available on %s: %w", c.host, err)
	}
	return nil
}

// ListContainers returns running containers.
func (c *DockerClient) ListContainers(ctx context.Context) ([]Container, error) {
	out, err := c.ssher(ctx, "docker", "ps", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var containers []Container
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var raw dockerpsOutput
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		containers = append(containers, Container{
			ID:     raw.ID,
			Name:   strings.TrimPrefix(raw.Names, "/"),
			Image:  raw.Image,
			Status: raw.Status,
			State:  raw.State,
			Ports:  raw.Ports,
		})
	}
	return containers, nil
}

// ListAllContainers returns all containers (including stopped).
func (c *DockerClient) ListAllContainers(ctx context.Context) ([]Container, error) {
	out, err := c.ssher(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var containers []Container
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var raw dockerpsOutput
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		containers = append(containers, Container{
			ID:     raw.ID,
			Name:   strings.TrimPrefix(raw.Names, "/"),
			Image:  raw.Image,
			Status: raw.Status,
			State:  raw.State,
			Ports:  raw.Ports,
		})
	}
	return containers, nil
}

// InspectContainer returns detailed container information.
func (c *DockerClient) InspectContainer(ctx context.Context, name string) (*ContainerDetail, error) {
	out, err := c.ssher(ctx, "docker", "inspect", name)
	if err != nil {
		return nil, err
	}
	var raw []dockerInspectOutput
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse docker inspect output: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("container %q not found", name)
	}
	d := &raw[0]

	detail := &ContainerDetail{
		Container: Container{
			Name:   strings.TrimPrefix(d.Name, "/"),
			Image:  d.Config.Image,
			Status: d.State.Status,
			State:  d.State.Status,
		},
		ID:       d.Id,
		Env:      d.Config.Env,
		Cmd:      d.Config.Cmd,
		Networks: []string{},
		Ports:    []PortMapping{},
	}

	// Mounts
	for _, m := range d.Mounts {
		detail.Mounts = append(detail.Mounts, fmt.Sprintf("%s:%s", m.Source, m.Destination))
	}

	// Networks
	for net := range d.NetworkSettings.Networks {
		detail.Networks = append(detail.Networks, net)
	}

	// Ports
	for contPort, bindings := range d.HostConfig.PortBindings {
		parts := strings.Split(contPort, "/")
		protocol := "tcp"
		if len(parts) > 1 {
			protocol = parts[1]
		}
		for _, b := range bindings {
			detail.Ports = append(detail.Ports, PortMapping{
				HostIP:   b.HostIP,
				HostPort: b.HostPort,
				ContPort: parts[0],
				Protocol: protocol,
			})
		}
	}

	return detail, nil
}

// ContainerLogs returns container log output.
func (c *DockerClient) ContainerLogs(ctx context.Context, name string, tail int, timestamps bool) (string, error) {
	tailStr := strconv.Itoa(tail)
	var out string
	var err error
	if timestamps {
		out, err = c.ssher(ctx, "docker", "logs", "--tail", tailStr, "-t", name)
	} else {
		out, err = c.ssher(ctx, "docker", "logs", "--tail", tailStr, name)
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

// ContainerStats returns live CPU and memory stats for a container.
func (c *DockerClient) ContainerStats(ctx context.Context, name string) (*ContainerStats, error) {
	// Use docker stats --no-stream to get a single snapshot
	out, err := c.ssher(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", name)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return nil, errors.New("no stats returned")
	}
	var raw struct {
		CPUPerc string `json:"CPUPerc"`
		MemPerc string `json:"MemPerc"`
		MemUsage string `json:"MemUsage"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse stats output: %w", err)
	}

	cpu, _ := strconv.ParseFloat(strings.TrimSuffix(raw.CPUPerc, "%"), 64)
	memPct, _ := strconv.ParseFloat(strings.TrimSuffix(raw.MemPerc, "%"), 64)

	// MemUsage is "used / limit" format like "547.5MiB / 11.66GiB"
	usedBytes, limitBytes := parseMemUsage(raw.MemUsage)

	return &ContainerStats{
		CPUPercent: cpu,
		Memory: MemoryStats{
			Used:    usedBytes,
			Limit:   limitBytes,
			Percent: memPct,
		},
	}, nil
}

// parseMemUsage parses "used / limit" format like "547.5MiB / 11.66GiB".
func parseMemUsage(s string) (usedBytes, limitBytes uint64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	usedStr := strings.TrimSpace(parts[0])
	limitStr := strings.TrimSpace(parts[1])

	usedBytes = parseSizeToBytes(usedStr)
	limitBytes = parseSizeToBytes(limitStr)
	return usedBytes, limitBytes
}

// parseSizeToBytes converts a size string like "547.5MiB" or "11.66GiB" to bytes.
func parseSizeToBytes(s string) uint64 {
	s = strings.TrimSpace(s)
	// Format is like "562.7MiB" or "11.66GiB" - unit is attached to number with no space
	// Find where the alphabetic unit starts by scanning backwards
	i := len(s) - 1
	for i >= 0 && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
		i--
	}
	i++ // move past the last letter
	valStr := s[:i]
	unit := s[i:]

	val, _ := strconv.ParseFloat(valStr, 64)
	var mult uint64 = 1
	switch strings.ToUpper(unit) {
	case "B", "BYTES":
		mult = 1
	case "KB", "KIB":
		mult = 1024
	case "MB", "MIB":
		mult = 1024 * 1024
	case "GB", "GIB":
		mult = 1024 * 1024 * 1024
	case "TB", "TIB":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return uint64(val * float64(mult))
}

// StartContainer starts a container.
func (c *DockerClient) StartContainer(ctx context.Context, name string) error {
	_, err := c.ssher(ctx, "docker", "start", name)
	return err
}

// StopContainer stops a container.
func (c *DockerClient) StopContainer(ctx context.Context, name string) error {
	_, err := c.ssher(ctx, "docker", "stop", name)
	return err
}

// RestartContainer restarts a container.
func (c *DockerClient) RestartContainer(ctx context.Context, name string) error {
	_, err := c.ssher(ctx, "docker", "restart", name)
	return err
}

// PauseContainer pauses a container.
func (c *DockerClient) PauseContainer(ctx context.Context, name string) error {
	_, err := c.ssher(ctx, "docker", "pause", name)
	return err
}

// UnpauseContainer unpauses a container.
func (c *DockerClient) UnpauseContainer(ctx context.Context, name string) error {
	_, err := c.ssher(ctx, "docker", "unpause", name)
	return err
}

// ListImages returns docker images.
func (c *DockerClient) ListImages(ctx context.Context) ([]Image, error) {
	out, err := c.ssher(ctx, "docker", "images", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var images []Image
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var raw dockerImageOutput
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		size, _ := parseDockerSize(raw.Size)
		images = append(images, Image{
			Repository: raw.Repository,
			Tag:        raw.Tag,
			ID:         raw.ID,
			Size:       size,
			CreatedAt:  raw.CreatedAt,
		})
	}
	return images, nil
}

func parseDockerSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	var mult uint64 = 1
	if strings.HasSuffix(s, "B") && !strings.HasPrefix(s, "B") {
		s = strings.TrimSuffix(s, "B")
		if strings.HasSuffix(s, "K") {
			mult = 1024
			s = strings.TrimSuffix(s, "K")
		} else if strings.HasSuffix(s, "M") {
			mult = 1024 * 1024
			s = strings.TrimSuffix(s, "M")
		} else if strings.HasSuffix(s, "G") {
			mult = 1024 * 1024 * 1024
			s = strings.TrimSuffix(s, "G")
		} else if strings.HasSuffix(s, "T") {
			mult = 1024 * 1024 * 1024 * 1024
			s = strings.TrimSuffix(s, "T")
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return uint64(v * float64(mult)), nil
}

// ContainerExists checks if a container name/id exists.
func (c *DockerClient) ContainerExists(ctx context.Context, name string) (bool, error) {
	_, err := c.ssher(ctx, "docker", "inspect", "--type=container", name)
	if err != nil {
		if strings.Contains(err.Error(), "no such object") || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ParseLogOutput splits docker log output into timestamp and message lines.
func ParseLogOutput(output string) []ContainerLogEntry {
	var entries []ContainerLogEntry
	// Docker logs format: "2024-01-01T00:00:00.000000000Z message"
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		entry := ContainerLogEntry{Message: line}
		// Try to parse timestamp prefix
		if len(line) > 30 && line[4] == '-' && line[7] == '-' {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				if t, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
					entry.Timestamp = t
					entry.Message = parts[1]
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

// URLEncodeName URL-encodes a container name for use in API paths.
func URLEncodeName(name string) string {
	return url.QueryEscape(strings.TrimPrefix(name, "/"))
}
