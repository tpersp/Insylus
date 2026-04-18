package devices

import (
	"fmt"
	"os"
	"time"

	"insylus/internal/format"
	"insylus/internal/shared"
)

func PrintInfoTable(items []shared.DeviceInventoryInfo) {
	w := format.NewTable(os.Stdout)
	fmt.Fprintln(w, "NAME\tMODE\tTYPE\tPURPOSE\tPARENT\tAGENT\tSSH\tACCESS\tSTATUS\tLAST SEEN\tIP")
	for _, item := range items {
		status := "pending/error"
		if item.Access.EnforcementSucceeded {
			status = "ok"
		}
		lastSeen := "never"
		if !item.Identity.LastSeenAt.IsZero() {
			lastSeen = item.Identity.LastSeenAt.Local().Format(time.DateTime)
		}
		ip := "-"
		if len(item.Identity.IPs) > 0 {
			ip = item.Identity.IPs[0]
		}
		sshValue := item.Connection.SSHAlias
		if sshValue == "" {
			sshValue = "-"
		}
		parent := "Unknown"
		if item.Topology.ParentState == shared.ParentStateLinked && item.Topology.ParentName != "" {
			parent = item.Topology.ParentName
		}
		if item.Topology.ParentState == shared.ParentStateNone {
			parent = "None"
		}
		purpose := string(item.Topology.Purpose)
		if purpose == "" || purpose == string(shared.DevicePurposeUnknown) {
			purpose = "-"
		}
		agentState := item.Agent.Version
		if agentState == "" {
			agentState = "unknown"
		}
		if item.Agent.AutoUpdate.UpdateAvailable {
			agentState += " update"
		}
		if item.Agent.AutoUpdate.Status == shared.AgentUpdateStatusFailed {
			agentState += " failed"
		}
		mode := "inventory"
		if item.Access.DeviceMode == shared.DeviceModeAccessManaged {
			mode = "managed"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Identity.Name, mode, item.Topology.DeviceType, purpose, parent, agentState, sshValue, item.Access.AccessMode, status, lastSeen, ip)
	}
	_ = w.Flush()
}
