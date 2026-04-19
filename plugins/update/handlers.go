package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/version"
)

type runtime struct {
	store   store
	client  *GitHubClient
	version string
	render  func(http.ResponseWriter, string, any)
}

func newRuntime(host pluginhost.Host) runtime {
	return runtime{
		store:   newStore(host),
		client:  NewGitHubClient(),
		version: version.AgentVersion,
		render:  host.Web().Render,
	}
}

// handleUpdatePage serves the update page.
func (rt runtime) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	autoUpdate, _, _, skippedVersion, _ := rt.store.GetUpdateSettings(r.Context())
	updates, _ := rt.store.ListUpdates(r.Context())
	rt.render(w, "update.html", map[string]any{
		"CurrentVersion": rt.version,
		"AutoUpdate":     autoUpdate,
		"SkippedVersion": skippedVersion,
		"Updates":        updates,
	})
}

// handleCheckUpdate handles the check for updates API.
func (rt runtime) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	release, err := rt.client.FetchLatestRelease(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to check for updates: " + err.Error(),
		})
		return
	}

	latestVersion := ExtractVersionFromTag(release.TagName)
	_, _, _, skippedVersion, _ := rt.store.GetUpdateSettings(r.Context())

	if err := rt.store.SetLastCheckedAt(r.Context()); err != nil {
		// Non-fatal, continue
	}

	updateAvailable := latestVersion != rt.version && latestVersion != skippedVersion

	response := UpdateCheckResponse{
		CurrentVersion:  rt.version,
		LatestVersion:   latestVersion,
		UpdateAvailable: updateAvailable,
		ReleaseNotes:    ParseReleaseNotes(release.Body),
		DownloadURL:     rt.client.GetDownloadURL(release.TagName),
		ChecksumURL:     rt.client.GetChecksumURL(release.TagName),
		PublishedAt:     release.PublishedAt,
		SkippedVersion:  skippedVersion,
	}

	writeJSON(w, http.StatusOK, response)
}

// handleApplyUpdate handles the apply update API.
func (rt runtime) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Version == "" {
		http.Error(w, "Version is required", http.StatusBadRequest)
		return
	}

	// Create update record
	id, err := rt.store.CreateUpdate(r.Context(), req.Version, time.Now().UTC().Format(time.RFC3339), "pending", "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to create update record: " + err.Error(),
		})
		return
	}

	// Run update in background since it takes time
	go func() {
		ctx := context.Background()
		rt.performUpdate(ctx, id, req.Version)
	}()

	writeJSON(w, http.StatusOK, ApplyUpdateResponse{
		Status:  "started",
		Message: "Update started. Check status on the update page.",
	})
}

// performUpdate performs the actual update process.
func (rt runtime) performUpdate(ctx context.Context, updateID int64, version string) {
	tagName := "v" + version

	// Step 1: Download the binary
	downloadURL := rt.client.GetDownloadURL(tagName)
	checksumURL := rt.client.GetChecksumURL(tagName)

	binaryData, err := rt.client.DownloadFile(ctx, downloadURL)
	if err != nil {
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	// Step 2: Download and verify checksum
	checksumData, err := rt.client.DownloadFile(ctx, checksumURL)
	if err != nil {
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	if err := rt.client.VerifyChecksum(binaryData, string(checksumData)); err != nil {
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	rt.store.UpdateUpdateStatus(ctx, updateID, "downloaded")

	// Step 3: Backup current binary
	currentBinaryPath := filepath.Join(InsylusDir, InsylusBinaryName)
	backupBinaryPath := currentBinaryPath + ".backup"

	if _, err := os.Stat(currentBinaryPath); err == nil {
		if err := copyFile(currentBinaryPath, backupBinaryPath); err != nil {
			rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
			return
		}
	}

	// Step 4: Stop the service
	if err := rt.stopService(); err != nil {
		// Try to restore backup and restart
		os.Rename(backupBinaryPath, currentBinaryPath)
		rt.startService()
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	// Step 5: Replace the binary
	if err := os.WriteFile(currentBinaryPath, binaryData, 0755); err != nil {
		// Restore backup
		os.Rename(backupBinaryPath, currentBinaryPath)
		rt.startService()
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	// Step 6: Clean up backup
	os.Remove(backupBinaryPath)

	// Step 7: Start the service
	if err := rt.startService(); err != nil {
		// Try to restore backup
		binaryDataOld, _ := os.ReadFile(backupBinaryPath)
		if binaryDataOld != nil {
			os.WriteFile(currentBinaryPath, binaryDataOld, 0755)
		}
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	// Step 8: Verify health check
	time.Sleep(2 * time.Second)
	if !rt.healthCheck() {
		rt.store.UpdateUpdateStatus(ctx, updateID, "failed")
		return
	}

	rt.store.UpdateUpdateStatus(ctx, updateID, "applied")
}

// stopService stops the insylus service.
func (rt runtime) stopService() error {
	cmd := exec.Command("systemctl", "stop", "insylus.service")
	return cmd.Run()
}

// startService starts the insylus service.
func (rt runtime) startService() error {
	cmd := exec.Command("systemctl", "start", "insylus.service")
	return cmd.Run()
}

// healthCheck checks if the service is healthy.
func (rt runtime) healthCheck() bool {
	// Simple health check by checking if the binary responds
	cmd := exec.Command("systemctl", "is-active", "insylus.service")
	err := cmd.Run()
	return err == nil
}

// handleSkipVersion handles skipping a version.
func (rt runtime) handleSkipVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := rt.store.SetSkippedVersion(r.Context(), req.Version); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAutoUpdateToggle handles toggling auto-update setting.
func (rt runtime) handleAutoUpdateToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := rt.store.SetAutoUpdateEnabled(r.Context(), req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleUpdateHistory handles getting update history.
func (rt runtime) handleUpdateHistory(w http.ResponseWriter, r *http.Request) {
	updates, err := rt.store.ListUpdates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updates)
}

// handleRollback handles rolling back to a previous version.
func (rt runtime) handleRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Update ID is required", http.StatusBadRequest)
		return
	}

	// Find the update record
	updates, err := rt.store.ListUpdates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var targetUpdate *UpdateStatus
	for _, u := range updates {
		if fmt.Sprintf("%d", u.ID) == id {
			targetUpdate = &u
			break
		}
	}

	if targetUpdate == nil {
		http.Error(w, "Update not found", http.StatusNotFound)
		return
	}

	if targetUpdate.Status != "applied" && targetUpdate.Status != "failed" {
		http.Error(w, "Can only rollback to applied or failed updates", http.StatusBadRequest)
		return
	}

	// Check if backup exists
	backupBinaryPath := filepath.Join(InsylusDir, InsylusBinaryName+".backup")
	if _, err := os.Stat(backupBinaryPath); os.IsNotExist(err) {
		http.Error(w, "No backup found to rollback to", http.StatusBadRequest)
		return
	}

	// Perform rollback
	currentBinaryPath := filepath.Join(InsylusDir, InsylusBinaryName)

	// Stop service
	if err := rt.stopService(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to stop service"})
		return
	}

	// Restore backup
	binaryData, err := os.ReadFile(backupBinaryPath)
	if err != nil {
		rt.startService()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to read backup"})
		return
	}

	if err := os.WriteFile(currentBinaryPath, binaryData, 0755); err != nil {
		rt.startService()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to restore backup"})
		return
	}

	os.Remove(backupBinaryPath)

	// Start service
	if err := rt.startService(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start service"})
		return
	}

	// Update record
	rt.store.UpdateUpdateStatus(r.Context(), targetUpdate.ID, "rolled_back")

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON writes JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// CopyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return dstFile.Sync()
}
