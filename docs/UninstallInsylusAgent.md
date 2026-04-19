# Uninstall Insylus Agent From Devices

This guide removes the Insylus device agent from a Linux host.

It removes the local service, binary, and agent config from the device. It does not delete the target record from the Insylus controller. You can keep that record as manual inventory or delete it from Insylus afterward.

## Standard Uninstall

Run these commands on the device where `insylus-agent` is installed:

```bash
sudo systemctl disable --now insylus-agent.service 2>/dev/null || true
sudo rm -f /etc/systemd/system/insylus-agent.service
sudo systemctl daemon-reload
sudo systemctl reset-failed insylus-agent.service 2>/dev/null || true
sudo rm -f /usr/local/bin/insylus-agent
sudo rm -f /usr/local/bin/.insylus-agent.update-*
sudo rm -rf /etc/insylus-agent
```

This removes:

- `insylus-agent.service`
- `/usr/local/bin/insylus-agent`
- any temporary auto-update replacement file left beside the binary
- `/etc/insylus-agent/config.json`

If the host was installed with custom agent paths, replace the service name, unit path, binary path, update temp glob, and config directory with the matching values used during install:

```bash
AGENT_SERVICE=insylus-agent.service
AGENT_UNIT=/etc/systemd/system/insylus-agent.service
AGENT_BIN=/usr/local/bin/insylus-agent
AGENT_CONFIG_DIR=/etc/insylus-agent
```

## Verify Removal

Check that the service and files are gone:

```bash
systemctl status --no-pager insylus-agent.service
command -v insylus-agent || true
test ! -e /etc/insylus-agent/config.json && echo "agent config removed"
```

Expected result:

- `systemctl` reports that `insylus-agent.service` could not be found or is not loaded.
- `command -v insylus-agent` prints nothing.
- The config check prints `agent config removed`.

## Optional Access Cleanup

Only use this section if the device was managed by the Access plugin. Inventory-only devices do not create, lock, unlock, or remove users, SSH keys, sudoers, or groups.

If possible, disable managed access for the target in Insylus first, then let the agent check in once before uninstalling it. That gives the controller a chance to send a normal cleanup policy.

If the agent is already removed, clean up manually.

Set the managed user. New installs default to `bob` unless you configured a different managed account:

```bash
MANAGED_USER=bob
```

Remove Insylus sudoers files:

```bash
sudo rm -f "/etc/sudoers.d/insylus-${MANAGED_USER}"
sudo rm -f "/etc/sudoers.d/insylus-${MANAGED_USER}-audit-readme"
```

Remove Insylus-managed SSH keys from the managed user's account. Edit the file and remove only the block between the Insylus markers:

```bash
sudoedit "/home/${MANAGED_USER}/.ssh/authorized_keys"
```

Remove this block if it exists:

```text
# insylus-managed-key begin
...
# insylus-managed-key end
```

If the managed user was created only for Insylus and nothing else uses it, remove the account:

```bash
sudo userdel -r "${MANAGED_USER}"
```

If you want to keep the account but disable login, lock it instead:

```bash
sudo usermod --lock "${MANAGED_USER}"
```

If the user was added to audit-related groups only for Insylus, you can remove those memberships. These commands are safe to ignore if the user is not a member:

```bash
sudo gpasswd -d "${MANAGED_USER}" adm 2>/dev/null || true
sudo gpasswd -d "${MANAGED_USER}" systemd-journal 2>/dev/null || true
```

## Remove From Multiple Devices

Replace the hostnames in this loop with the devices you want to clean:

```bash
for host in node1 node2 node3; do
  ssh "$host" '
    sudo systemctl disable --now insylus-agent.service 2>/dev/null || true
    sudo rm -f /etc/systemd/system/insylus-agent.service
    sudo systemctl daemon-reload
    sudo systemctl reset-failed insylus-agent.service 2>/dev/null || true
    sudo rm -f /usr/local/bin/insylus-agent
    sudo rm -f /usr/local/bin/.insylus-agent.update-*
    sudo rm -rf /etc/insylus-agent
  '
done
```

## Controller Cleanup

After uninstalling the agent, the Insylus controller may still show the old target. That is normal. The local agent is gone, but the inventory record remains until you remove it.

You can either:

- keep the target as a manual inventory record
- delete the target from the Insylus UI
- delete it through the target API

Example API removal:

```bash
curl -X DELETE "http://127.0.0.1:8080/api/targets/<target-id>"
```

## Reinstall Later

To enroll the same device again, create or open a target in Insylus and run the current install command from the web UI. The new install writes a fresh `/etc/insylus-agent/config.json` and registers a new agent token.
