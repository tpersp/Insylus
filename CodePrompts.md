# Code Prompts

---

## Fix Access Plugin Web UI Issues

Fix the Access plugin web UI issues in internal/server/templates/access_settings.html:

1. Fix the "How It Works → Managed User Behavior" section: span/strong pairs render without spacing (e.g., "Controller service userinsylus" instead of "Controller service user: insylus"). Add proper visual separation between labels and values.

2. The "Managed user" input field is pre-filled with the controller service account (e.g., `insylus`) from ManagedAccount.ManagedUser. Change it to be blank by default, or add placeholder text that clearly indicates this is for the *remote* managed account, not the controller account.

3. Replace the "Audit groups" free-text input with a dropdown containing common groups (sudo, wheel, adm, docker) plus a "Custom..." option that reveals a text field for non-standard group names.

**After completing the changes:**
1. Verify the build succeeds: `go build ./...`
2. Deploy, install, and restart the app:
   ```bash
   go test ./...
   go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
   go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl
   go build -o /opt/insylus/dist/insylus-agent ./cmd/insylus-agent
   env GOOS=linux GOARCH=amd64 go build -o /opt/insylus/dist/insylus-agent-linux-amd64 ./cmd/insylus-agent
   env GOOS=linux GOARCH=arm64 go build -o /opt/insylus/dist/insylus-agent-linux-arm64 ./cmd/insylus-agent
   env GOOS=linux GOARCH=arm GOARM=7 go build -o /opt/insylus/dist/insylus-agent-linux-armv7 ./cmd/insylus-agent
   sudo bash /opt/insylus/scripts/install-insylus-service.sh
   sudo systemctl restart insylus.service
   insylusctl devices
   ```
3. Mark the completed items in plans/TODO.md with `[x]`