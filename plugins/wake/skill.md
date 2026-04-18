## Wake plugin

The wake plugin sends Wake-on-LAN magic packets to devices that support it.

### Rules

- WoL is only sent when the device has `wol.enabled=true` in inventory.
- Devices seen recently (within the last 45 seconds) are reported as `already online` — no packet is sent.
- WoL packets are broadcast to the device's MAC address, not sent to a specific IP.

### CLI commands

```bash
insylusctl wake DEVICE [--json]
```

`DEVICE` is a required positional argument — device name, hostname, IP, or ID.

Without `--json`: prints a human-readable sentence ("Sent WoL magic packet to device" or "device is already online").

With `--json`: prints `{"status":"already_online"}` or `{"status":"sent"}`.

### API endpoint

```bash
POST /api/devices/<device-id>/wake
```

Returns: `{"status":"already_online"}` or `{"status":"sent"}`