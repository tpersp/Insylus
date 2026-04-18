package docker

import "time"

// Container represents a Docker container summary.
type Container struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Status    string `json:"status"`
	State     string `json:"state"` // running, exited, paused, restarting
	Ports     string `json:"ports"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

// ContainerDetail represents detailed container information.
type ContainerDetail struct {
	Container
	ID       string        `json:"id"`
	Env      []string      `json:"env"`
	Cmd      []string      `json:"cmd"`
	Mounts   []string      `json:"mounts"`
	Networks []string      `json:"networks"`
	Ports    []PortMapping `json:"ports_detail"`
}

// PortMapping represents a port mapping from host to container.
type PortMapping struct {
	HostIP   string `json:"host_ip"`
	HostPort string `json:"host_port"`
	ContPort string `json:"cont_port"`
	Protocol string `json:"protocol"`
}

// ContainerStats holds CPU and memory stats for a container.
type ContainerStats struct {
	CPUPercent float64     `json:"cpu_percent"`
	Memory     MemoryStats `json:"memory"`
}

// MemoryStats holds memory usage statistics.
type MemoryStats struct {
	Used    uint64  `json:"used"`
	Limit   uint64  `json:"limit"`
	Percent float64 `json:"percent"`
}

// Image represents a Docker image.
type Image struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	ID         string `json:"id"`
	Size       uint64 `json:"size"`
	CreatedAt  string `json:"created_at"`
}

// ContainerLogEntry represents a single log line.
type ContainerLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// DockerNode represents a configured Docker host.
type DockerNode struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Hostname   string `json:"hostname"`
	DockerHost string `json:"docker_host"`
	SSHUser    string `json:"ssh_user,omitempty"`
	HasDocker  bool   `json:"has_docker"`
}

// ContainerListResponse is the API response for container listing.
type ContainerListResponse struct {
	Node       string      `json:"node"`
	Containers []Container `json:"containers"`
}

// ImageListResponse is the API response for image listing.
type ImageListResponse struct {
	Node   string  `json:"node"`
	Images []Image `json:"images"`
}

// ActionResult is the result of a container action.
type ActionResult struct {
	Action    string `json:"action"`
	Container string `json:"container"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// DockerNodeList is the response for listing Docker nodes.
type DockerNodeList struct {
	Nodes []DockerNode `json:"nodes"`
}

// dockerpsOutput is the raw output format from `docker ps --format json`.
type dockerpsOutput struct {
	ID        string `json:"ID"`
	Names     string `json:"Names"`
	Image     string `json:"Image"`
	Status    string `json:"Status"`
	State     string `json:"State"`
	Ports     string `json:"Ports"`
	CreatedAt string `json:"CreatedAt"`
}

// dockerInspectOutput is the raw output from `docker inspect`.
type dockerInspectOutput struct {
	Id     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image string   `json:"Image"`
		Env   []string `json:"Env"`
		Cmd   []string `json:"Cmd"`
	} `json:"Config"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		Restarting bool   `json:"Restarting"`
	} `json:"State"`
	HostConfig struct {
		PortBindings map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Type        string `json:"Type"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// dockerImageOutput is the raw output from `docker images --format json`.
type dockerImageOutput struct {
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	ID         string `json:"ID"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
}
