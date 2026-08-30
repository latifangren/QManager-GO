package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/config"
)

const (
	CurrentQManagerVersion = "v0.1.14"
	DefaultGitHubRepo      = "iamromulan/quecmanager-core"
	DefaultUpdateLockFile  = "/tmp/qmanager_update.lock"
	DefaultUpdateDir       = "/tmp/qmanager_update"
	DefaultBinaryPath      = "/usr/bin/qmanager"
)

// UpdateSettings holds update preferences.
type UpdateSettings struct {
	AutoUpdateEnabled bool   `json:"auto_update_enabled"`
	AutoUpdateTime    string `json:"auto_update_time"`
	IncludePrerelease bool   `json:"include_prerelease"`
}

// DownloadState represents staged or active download info.
type DownloadState struct {
	Status  string `json:"status"` // "idle", "downloading", "ready", "error", "installing", "rebooting"
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	Size    string `json:"size,omitempty"`
}

// UpdateResponse represents response for GET /system/update.sh
type UpdateResponse struct {
	Success          bool           `json:"success"`
	CurrentVersion   string         `json:"current_version"`
	LatestVersion    *string        `json:"latest_version"`
	UpdateAvailable  bool           `json:"update_available"`
	Changelog        *string        `json:"changelog"`
	CurrentChangelog *string        `json:"current_changelog"`
	DownloadURL      *string        `json:"download_url"`
	DownloadSize     *string        `json:"download_size"`
	PublishedAt      *string        `json:"published_at"`
	IsPrerelease     bool           `json:"is_prerelease"`
	Settings         UpdateSettings `json:"settings"`
	Download         *DownloadState `json:"download,omitempty"`
	PendingInstall   bool           `json:"pending_install_found"`
	PendingVersion   *string        `json:"pending_version"`
	Error            string         `json:"error,omitempty"`
	Detail           string         `json:"detail,omitempty"`
}

// GitHubRelease represents GitHub API release response.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset represents release asset metadata.
type GitHubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateHandler manages software updates and OTA.
type UpdateHandler struct {
	cfgMgr       *config.Manager
	httpClient   *http.Client
	githubRepo   string
	lockFile     string
	updateDir    string
	binaryPath   string
	downloadMu   sync.Mutex
	currentDown  *DownloadState
	activeCancel context.CancelFunc
}

// NewUpdateHandler creates a new UpdateHandler.
func NewUpdateHandler(cfgMgr *config.Manager) *UpdateHandler {
	repo := os.Getenv("QMANAGER_GITHUB_REPO")
	if repo == "" {
		repo = DefaultGitHubRepo
	}
	bin := os.Getenv("QMANAGER_BINARY_PATH")
	if bin == "" {
		bin = DefaultBinaryPath
	}

	return &UpdateHandler{
		cfgMgr:      cfgMgr,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		githubRepo:  repo,
		lockFile:    DefaultUpdateLockFile,
		updateDir:   DefaultUpdateDir,
		binaryPath:  bin,
		currentDown: &DownloadState{Status: "idle"},
	}
}

// GetCurrentVersion returns current compiled/configured version.
func (h *UpdateHandler) GetCurrentVersion() string {
	ver := os.Getenv("QMANAGER_VERSION")
	if ver != "" {
		return ver
	}
	return CurrentQManagerVersion
}

// CheckUpdate handles GET /api/v1/system/update and GET /cgi-bin/quecmanager/system/update.sh
func (h *UpdateHandler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	if action == "status" || action == "download_status" {
		h.downloadMu.Lock()
		state := *h.currentDown
		h.downloadMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"status":  state.Status,
			"version": state.Version,
			"message": state.Message,
			"size":    state.Size,
		})
		return
	}

	if action == "cancel_download" {
		h.cancelActiveDownload()
		Success(w, map[string]interface{}{"success": true, "message": "Download cancelled"})
		return
	}

	cfg := h.cfgMgr.Get()
	includePrerelease := cfg.Update.IncludePrerelease == 1
	currentVer := h.GetCurrentVersion()

	// Check GitHub releases
	rel, err := h.fetchLatestRelease(r.Context(), includePrerelease)

	settings := UpdateSettings{
		AutoUpdateEnabled: cfg.Update.AutoUpdateEnabled == 1,
		AutoUpdateTime:    cfg.Update.AutoUpdateTime,
		IncludePrerelease: cfg.Update.IncludePrerelease == 1,
	}
	if settings.AutoUpdateTime == "" {
		settings.AutoUpdateTime = "03:00"
	}

	resp := UpdateResponse{
		Success:        true,
		CurrentVersion: currentVer,
		Settings:       settings,
	}

	// Check if staged download is ready in updateDir
	stagedVer, stagedFile := h.checkStagedDownload()
	if stagedVer != "" {
		resp.PendingInstall = true
		resp.PendingVersion = &stagedVer
		if fi, err := os.Stat(stagedFile); err == nil {
			sizeMB := fmt.Sprintf("%.1f MB", float64(fi.Size())/(1024*1024))
			resp.Download = &DownloadState{
				Status:  "ready",
				Version: stagedVer,
				Message: fmt.Sprintf("Download verified (%s)", sizeMB),
				Size:    sizeMB,
			}
		}
	} else {
		h.downloadMu.Lock()
		if h.currentDown.Status != "idle" {
			st := *h.currentDown
			resp.Download = &st
		}
		h.downloadMu.Unlock()
	}

	if err != nil || rel == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	latestVer := rel.TagName
	resp.LatestVersion = &latestVer
	resp.Changelog = &rel.Body
	resp.PublishedAt = &rel.PublishedAt
	resp.IsPrerelease = rel.Prerelease

	// Semver compare
	if CompareSemver(latestVer, currentVer) > 0 {
		resp.UpdateAvailable = true
	}

	// Find download asset URL
	for _, asset := range rel.Assets {
		if strings.HasSuffix(asset.Name, ".tar.gz") || strings.HasSuffix(asset.Name, ".zip") || strings.Contains(asset.Name, "qmanager") {
			url := asset.BrowserDownloadURL
			resp.DownloadURL = &url
			sizeStr := fmt.Sprintf("%.1f MB", float64(asset.Size)/(1024*1024))
			resp.DownloadSize = &sizeStr
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleUpdateAction handles POST /api/v1/system/update and POST /cgi-bin/quecmanager/system/update.sh
func (h *UpdateHandler) HandleUpdateAction(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Action            string `json:"action"`
		Version           string `json:"version"`
		URL               string `json:"url"`
		AutoUpdateEnabled *bool  `json:"auto_update_enabled"`
		AutoUpdateTime    string `json:"auto_update_time"`
		IncludePrerelease *bool  `json:"include_prerelease"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "save_settings", "save":
		h.handleSaveSettings(w, payload.AutoUpdateEnabled, payload.AutoUpdateTime, payload.IncludePrerelease)
	case "download":
		h.handleDownload(w, payload.Version, payload.URL)
	case "apply", "install":
		h.handleApply(w, payload.Version, payload.URL)
	case "cancel_download":
		h.cancelActiveDownload()
		Success(w, map[string]interface{}{"success": true, "message": "Download cancelled"})
	case "reboot_ack":
		Success(w, map[string]interface{}{"success": true, "message": "Reboot acknowledged"})
	default:
		Error(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", payload.Action))
	}
}

func (h *UpdateHandler) handleSaveSettings(w http.ResponseWriter, autoUpdateEnabled *bool, autoUpdateTime string, includePrerelease *bool) {
	err := h.cfgMgr.Update(func(c *config.Config) {
		if autoUpdateEnabled != nil {
			if *autoUpdateEnabled {
				c.Update.AutoUpdateEnabled = 1
			} else {
				c.Update.AutoUpdateEnabled = 0
			}
		}
		if autoUpdateTime != "" {
			c.Update.AutoUpdateTime = autoUpdateTime
		}
		if includePrerelease != nil {
			if *includePrerelease {
				c.Update.IncludePrerelease = 1
			} else {
				c.Update.IncludePrerelease = 0
			}
		}
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save update configuration")
		return
	}

	// Trigger qmanager_auto_update_arm if available
	cfg := h.cfgMgr.Get()
	if cfg.Update.AutoUpdateEnabled == 1 {
		_ = exec.Command("sudo", "-n", "/usr/bin/qmanager_auto_update_arm", "on").Run()
	} else {
		_ = exec.Command("sudo", "-n", "/usr/bin/qmanager_auto_update_arm", "off").Run()
	}

	Success(w, map[string]interface{}{
		"success": true,
		"settings": map[string]interface{}{
			"auto_update_enabled": cfg.Update.AutoUpdateEnabled == 1,
			"auto_update_time":    cfg.Update.AutoUpdateTime,
			"include_prerelease":  cfg.Update.IncludePrerelease == 1,
		},
	})
}

func (h *UpdateHandler) handleDownload(w http.ResponseWriter, version, downloadURL string) {
	h.downloadMu.Lock()
	if h.currentDown.Status == "downloading" {
		h.downloadMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"status":  "downloading",
			"message": "Download already in progress",
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.activeCancel = cancel
	h.currentDown = &DownloadState{
		Status:  "downloading",
		Version: version,
		Message: "Starting download...",
	}
	h.downloadMu.Unlock()

	go h.asyncDownload(ctx, version, downloadURL)

	Success(w, map[string]interface{}{
		"success": true,
		"status":  "downloading",
		"version": version,
		"message": "Download initiated",
	})
}

func (h *UpdateHandler) asyncDownload(ctx context.Context, version, downloadURL string) {
	if downloadURL == "" {
		// Resolve from GitHub if missing
		rel, err := h.fetchLatestRelease(ctx, true)
		if err == nil && rel != nil {
			for _, a := range rel.Assets {
				if strings.HasSuffix(a.Name, ".tar.gz") || strings.Contains(a.Name, "qmanager") {
					downloadURL = a.BrowserDownloadURL
					break
				}
			}
		}
	}

	if downloadURL == "" {
		h.downloadMu.Lock()
		h.currentDown = &DownloadState{
			Status:  "error",
			Version: version,
			Message: "No downloadable asset URL found",
		}
		h.downloadMu.Unlock()
		return
	}

	_ = os.MkdirAll(h.updateDir, 0755)
	destTar := filepath.Join(h.updateDir, fmt.Sprintf("qmanager-%s.tar.gz", version))

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		h.setDownloadError(version, err.Error())
		return
	}
	req.Header.Set("User-Agent", "QManager-OTA/1.0")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.setDownloadError(version, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.setDownloadError(version, fmt.Sprintf("Download failed: HTTP %d", resp.StatusCode))
		return
	}

	out, err := os.Create(destTar)
	if err != nil {
		h.setDownloadError(version, err.Error())
		return
	}
	defer out.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	buf := make([]byte, 32*1024)
	var downloaded int64
	total := resp.ContentLength

	for {
		select {
		case <-ctx.Done():
			_ = out.Close()
			_ = os.Remove(destTar)
			h.downloadMu.Lock()
			h.currentDown = &DownloadState{Status: "idle"}
			h.downloadMu.Unlock()
			return
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = writer.Write(buf[:n])
			downloaded += int64(n)
			if total > 0 {
				pct := int(float64(downloaded) / float64(total) * 100)
				h.downloadMu.Lock()
				h.currentDown.Message = fmt.Sprintf("Downloading... %d%%", pct)
				h.downloadMu.Unlock()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			h.setDownloadError(version, err.Error())
			return
		}
	}

	shaHex := hex.EncodeToString(hasher.Sum(nil))
	sizeMB := fmt.Sprintf("%.1f MB", float64(downloaded)/(1024*1024))

	// Write version tag
	_ = os.WriteFile(filepath.Join(h.updateDir, "staged_version"), []byte(version), 0644)
	_ = os.WriteFile(filepath.Join(h.updateDir, "staged_sha256"), []byte(shaHex), 0644)

	h.downloadMu.Lock()
	h.currentDown = &DownloadState{
		Status:  "ready",
		Version: version,
		Message: fmt.Sprintf("Download verified (%s)", sizeMB),
		Size:    sizeMB,
	}
	h.downloadMu.Unlock()
}

func (h *UpdateHandler) setDownloadError(version, msg string) {
	h.downloadMu.Lock()
	h.currentDown = &DownloadState{
		Status:  "error",
		Version: version,
		Message: msg,
	}
	h.downloadMu.Unlock()
}

func (h *UpdateHandler) cancelActiveDownload() {
	h.downloadMu.Lock()
	defer h.downloadMu.Unlock()
	if h.activeCancel != nil {
		h.activeCancel()
		h.activeCancel = nil
	}
	h.currentDown = &DownloadState{Status: "idle"}
}

func (h *UpdateHandler) handleApply(w http.ResponseWriter, version, downloadURL string) {
	stagedVer, stagedFile := h.checkStagedDownload()
	if stagedVer == "" || stagedFile == "" {
		Error(w, http.StatusBadRequest, "No staged update package found. Please download first.")
		return
	}

	h.downloadMu.Lock()
	h.currentDown = &DownloadState{
		Status:  "installing",
		Version: stagedVer,
		Message: "Extracting and applying update...",
	}
	h.downloadMu.Unlock()

	// Extract binary and replace
	extractedBin, err := extractBinaryFromTar(stagedFile, h.updateDir)
	if err != nil {
		h.setDownloadError(stagedVer, fmt.Sprintf("Extraction failed: %v", err))
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Extraction failed: %v", err))
		return
	}

	// Make executable
	_ = os.Chmod(extractedBin, 0755)

	// Replace active binary atomically if possible or copy
	backupPath := h.binaryPath + ".bak"
	_ = os.Rename(h.binaryPath, backupPath)
	if err := copyFile(extractedBin, h.binaryPath); err != nil {
		_ = os.Rename(backupPath, h.binaryPath) // Restore on failure
		h.setDownloadError(stagedVer, fmt.Sprintf("Binary replace failed: %v", err))
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Binary replace failed: %v", err))
		return
	}
	_ = os.Chmod(h.binaryPath, 0755)
	_ = os.RemoveAll(h.updateDir)

	Success(w, map[string]interface{}{
		"success": true,
		"status":  "rebooting",
		"message": "Update installed successfully. Service restarting...",
	})

	// Trigger graceful restart
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("systemctl", "restart", "qmanager").Run()
	}()
}

func (h *UpdateHandler) checkStagedDownload() (string, string) {
	verData, err := os.ReadFile(filepath.Join(h.updateDir, "staged_version"))
	if err != nil {
		return "", ""
	}
	ver := strings.TrimSpace(string(verData))
	if ver == "" {
		return "", ""
	}
	tarFile := filepath.Join(h.updateDir, fmt.Sprintf("qmanager-%s.tar.gz", ver))
	if _, err := os.Stat(tarFile); err == nil {
		return ver, tarFile
	}
	return "", ""
}

func (h *UpdateHandler) fetchLatestRelease(ctx context.Context, includePrerelease bool) (*GitHubRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases", h.githubRepo)
	if !includePrerelease {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", h.githubRepo)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "QManager-OTA/1.0")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	if !includePrerelease {
		var single GitHubRelease
		if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
			return nil, err
		}
		return &single, nil
	}

	var list []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	return &list[0], nil
}

// CompareSemver compares two version strings (e.g., "v0.1.15" vs "v0.1.14").
// Returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal.
func CompareSemver(v1, v2 string) int {
	cleanV1 := strings.TrimPrefix(strings.TrimSpace(v1), "v")
	cleanV2 := strings.TrimPrefix(strings.TrimSpace(v2), "v")

	parts1 := strings.SplitN(cleanV1, "-", 2)
	parts2 := strings.SplitN(cleanV2, "-", 2)

	nums1 := strings.Split(parts1[0], ".")
	nums2 := strings.Split(parts2[0], ".")

	maxLen := len(nums1)
	if len(nums2) > maxLen {
		maxLen = len(nums2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(nums1) {
			n1, _ = strconv.Atoi(nums1[i])
		}
		if i < len(nums2) {
			n2, _ = strconv.Atoi(nums2[i])
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	// Pre-release check: non-prerelease is newer than prerelease
	if len(parts1) == 1 && len(parts2) > 1 {
		return 1
	}
	if len(parts1) > 1 && len(parts2) == 1 {
		return -1
	}
	if len(parts1) > 1 && len(parts2) > 1 {
		if parts1[1] > parts2[1] {
			return 1
		} else if parts1[1] < parts2[1] {
			return -1
		}
	}

	return 0
}

func extractBinaryFromTar(tarPath, destDir string) (string, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var foundPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		cleanName := filepath.Base(header.Name)
		if header.Typeflag == tar.TypeReg && (cleanName == "qmanager" || strings.HasPrefix(cleanName, "qmanager-")) {
			target := filepath.Join(destDir, "qmanager.extracted")
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0755)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
			foundPath = target
			break
		}
	}

	if foundPath == "" {
		return "", fmt.Errorf("no qmanager binary found inside archive")
	}
	return foundPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
