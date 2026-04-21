package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"insylus/internal/pluginhost"
	"insylus/internal/version"
)

type runtime struct {
	store      store
	client     *GitHubClient
	version    string
	helperPath string
	stagingDir string
	render     func(http.ResponseWriter, string, any)
}

func newRuntime(host pluginhost.Host) runtime {
	return runtime{
		store:      newStore(host),
		client:     NewGitHubClient(),
		version:    version.ServerVersion,
		helperPath: envDefault("INSYLUS_UPDATE_HELPER_PATH", DefaultHelperPath),
		stagingDir: envDefault("INSYLUS_UPDATE_STAGING_DIR", DefaultStagingDir),
		render:     host.Web().Render,
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
		if errors.Is(err, ErrNoLatestRelease) {
			writeJSON(w, http.StatusOK, UpdateCheckResponse{
				CurrentVersion:  rt.version,
				LatestVersion:   "",
				UpdateAvailable: false,
				Message:         "No server updates are available yet.",
			})
			return
		}
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
	pkg, assetErr := ReleasePackageAssets(release)
	message := ""
	if updateAvailable && assetErr != nil {
		updateAvailable = false
		message = "A controller update is available, but the full update bundle is not ready yet."
	}

	response := UpdateCheckResponse{
		CurrentVersion:  rt.version,
		LatestVersion:   latestVersion,
		UpdateAvailable: updateAvailable,
		Message:         message,
		ReleaseNotes:    ParseReleaseNotes(release.Body),
		DownloadURL:     pkg.DownloadURL,
		ChecksumURL:     pkg.ChecksumURL,
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
	tagName := versionTag(req.Version)
	release, err := rt.client.FetchReleaseByTag(r.Context(), tagName)
	if err != nil {
		if errors.Is(err, ErrNoLatestRelease) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Update package is not available."})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to prepare update: " + err.Error(),
		})
		return
	}
	pkg, err := ReleasePackageAssets(release)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Update package is not available."})
		return
	}

	// Create update record
	id, err := rt.store.CreateUpdate(r.Context(), ExtractVersionFromTag(release.TagName), release.PublishedAt, "pending", "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to create update record: " + err.Error(),
		})
		return
	}

	// Run update in background since it takes time
	go func() {
		ctx := context.Background()
		rt.performUpdate(ctx, id, ExtractVersionFromTag(release.TagName), pkg)
	}()

	writeJSON(w, http.StatusOK, ApplyUpdateResponse{
		Status:  "started",
		Message: "Update started. Check status on the update page.",
	})
}

// performUpdate performs the actual update process.
func (rt runtime) performUpdate(ctx context.Context, updateID int64, version string, pkg ReleasePackage) {
	fail := func(format string, args ...any) {
		_ = rt.store.UpdateUpdateStatusNotes(ctx, updateID, "failed", fmt.Sprintf(format, args...))
	}

	bundleData, err := rt.client.DownloadFile(ctx, pkg.DownloadURL)
	if err != nil {
		fail("Download failed: %v", err)
		return
	}

	// Step 2: Download and verify checksum
	checksumData, err := rt.client.DownloadFile(ctx, pkg.ChecksumURL)
	if err != nil {
		fail("Checksum download failed: %v", err)
		return
	}

	if err := rt.client.VerifyChecksum(bundleData, string(checksumData)); err != nil {
		fail("Checksum verification failed: %v", err)
		return
	}

	rt.store.UpdateUpdateStatus(ctx, updateID, "downloaded")

	if err := os.MkdirAll(rt.stagingDir, 0750); err != nil {
		fail("Create update staging directory failed: %v", err)
		return
	}

	stagedBundlePath := filepath.Join(rt.stagingDir, pkg.AssetName)
	if err := os.WriteFile(stagedBundlePath, bundleData, 0644); err != nil {
		fail("Write staged update bundle failed: %v", err)
		return
	}

	stagedBundleDir := filepath.Join(rt.stagingDir, "bundle-"+version)
	if err := os.RemoveAll(stagedBundleDir); err != nil {
		fail("Reset staged bundle directory failed: %v", err)
		return
	}
	if err := extractTarGz(bundleData, stagedBundleDir); err != nil {
		fail("Extract update bundle failed: %v", err)
		return
	}

	if err := rt.validateStagedBundle(stagedBundleDir, version); err != nil {
		fail("%v", err)
		return
	}

	if err := rt.applyStagedBundle(stagedBundleDir); err != nil {
		fail("%v", err)
		return
	}

	rt.store.UpdateUpdateStatus(ctx, updateID, "applied")
	if err := rt.restartService(); err != nil {
		fail("%v", err)
	}
}

func (rt runtime) validateStagedBundle(dir, expectedVersion string) error {
	required := []string{"insylus-server", "insylusctl", "insylus-agent", "insylus-agent-linux-amd64", "insylus-agent-linux-arm64", "insylus-agent-linux-armv7"}
	for _, name := range required {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("validate staged bundle: missing %s", name)
		}
		if info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("validate staged bundle: invalid %s", name)
		}
	}
	cmd := exec.Command(filepath.Join(dir, "insylus-server"), "version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("validate staged server version: %w", err)
	}
	got := strings.TrimSpace(string(output))
	if got != expectedVersion {
		return fmt.Errorf("validate staged server version: got %q want %q", got, expectedVersion)
	}
	return nil
}

func (rt runtime) applyStagedBundle(path string) error {
	cmd := exec.Command("sudo", "-n", rt.helperPath, "apply", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apply controller update: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (rt runtime) restartService() error {
	cmd := exec.Command("sudo", "-n", rt.helperPath, "restart")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restart server: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (rt runtime) rollbackAppliedBundle() error {
	cmd := exec.Command("sudo", "-n", rt.helperPath, "rollback")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rollback controller update: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
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

	if err := rt.rollbackAppliedBundle(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to restore backup"})
		return
	}

	// Update record
	rt.store.UpdateUpdateStatus(r.Context(), targetUpdate.ID, "rolled_back")
	if err := rt.restartService(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to restart service"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON writes JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func versionTag(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
