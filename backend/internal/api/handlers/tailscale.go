package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TailscaleSelf reflects this device in the tailnet.
type TailscaleSelf struct {
	Hostname     string   `json:"hostname"`
	DNSName      string   `json:"dns_name"`
	TailscaleIPs []string `json:"tailscale_ips"`
	Online       bool     `json:"online"`
	OS           string   `json:"os"`
	Relay        string   `json:"relay"`
}

// TailscaleTailnet reflects the tailnet domain / MagicDNS.
type TailscaleTailnet struct {
	Name            string `json:"name"`
	MagicDNSSuffix  string `json:"magic_dns_suffix"`
	MagicDNSEnabled bool   `json:"magic_dns_enabled"`
}

// TailscalePeer reflects an individual machine on the tailnet.
type TailscalePeer struct {
	Hostname     string   `json:"hostname"`
	DNSName      string   `json:"dns_name"`
	TailscaleIPs []string `json:"tailscale_ips"`
	OS           string   `json:"os"`
	Online       bool     `json:"online"`
	LastSeen     string   `json:"last_seen"`
	Relay        string   `json:"relay"`
	ExitNode     bool     `json:"exit_node"`
}

// TailscaleStatus reflects the frontend use-tailscale.ts contract.
type TailscaleStatus struct {
	Success       bool              `json:"success"`
	Installed     bool              `json:"installed"`
	DaemonRunning bool              `json:"daemon_running"`
	EnabledOnBoot bool              `json:"enabled_on_boot"`
	SSHEnabled    bool              `json:"ssh_enabled"`
	Version       string            `json:"version,omitempty"`
	BackendState  string            `json:"backend_state,omitempty"`
	AuthURL       string            `json:"auth_url,omitempty"`
	Self          *TailscaleSelf    `json:"self,omitempty"`
	Tailnet       *TailscaleTailnet `json:"tailnet,omitempty"`
	Peers         []TailscalePeer   `json:"peers"`
	Health        []string          `json:"health"`
	InstallHint   string            `json:"install_hint,omitempty"`
	ErrorDetail   string            `json:"error_detail,omitempty"`
}

// TailscaleRawStatus models the CLI output of `tailscale status --json`.
type TailscaleRawStatus struct {
	Version      string `json:"Version"`
	BackendState string `json:"BackendState"`
	AuthURL      string `json:"AuthURL"`
	Self         struct {
		HostName     string   `json:"HostName"`
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		OS           string   `json:"OS"`
		Online       bool     `json:"Online"`
		Relay        string   `json:"Relay"`
	} `json:"Self"`
	Health         []string `json:"Health"`
	MagicDNSSuffix string   `json:"MagicDNSSuffix"`
	CurrentTailnet *struct {
		Name            string `json:"Name"`
		MagicDNSSuffix  string `json:"MagicDNSSuffix"`
		MagicDNSEnabled bool   `json:"MagicDNSEnabled"`
	} `json:"CurrentTailnet"`
	Peer map[string]struct {
		HostName     string   `json:"HostName"`
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		OS           string   `json:"OS"`
		Online       bool     `json:"Online"`
		LastSeen     string   `json:"LastSeen"`
		Relay        string   `json:"Relay"`
		ExitNode     bool     `json:"ExitNode"`
	} `json:"Peer"`
}

var (
	tailscaleInstallJSON = "/tmp/qmanager_tailscale_install.json"
	tailscaleInstallLog  = "/tmp/qmanager_tailscale_install.log"
)

const (
	tailscaleDir         = "/usrdata/tailscale"
	tailscaleVersion     = "1.92.5"
	tailscaleServiceUnit = "/lib/systemd/system/tailscaled.service"
)

// TailscaleHandler manages Tailscale VPN daemon interactions.
type TailscaleHandler struct {
	mu            sync.Mutex
	isInstalling  bool
	statusExec    func() ([]byte, error)
	systemctlExec func(action string) error
}

// NewTailscaleHandler creates a new TailscaleHandler.
func NewTailscaleHandler() *TailscaleHandler {
	return &TailscaleHandler{
		statusExec: func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
		},
		systemctlExec: func(action string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, "systemctl", action, "tailscaled").Run()
		},
	}
}

// isTailscaleInstalled checks if the binary exists in PATH or target directory.
func isTailscaleInstalled() bool {
	if _, err := exec.LookPath("tailscale"); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(tailscaleDir, "tailscale")); err == nil {
		return true
	}
	return false
}

// isTailscaleRunning checks if the tailscaled daemon is active.
func isTailscaleRunning() bool {
	out, err := exec.Command("systemctl", "is-active", "tailscaled").Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		return true
	}
	out, err = exec.Command("pidof", "tailscaled").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// isTailscaleBootEnabled checks if the service is enabled on boot.
func isTailscaleBootEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", "tailscaled").Output()
	return err == nil && strings.TrimSpace(string(out)) == "enabled"
}

// isSSHEnabled checks if Tailscale SSH is enabled in debug prefs.
func isSSHEnabled() bool {
	out, err := exec.Command("tailscale", "debug", "prefs").Output()
	if err == nil {
		var prefs struct {
			RunSSH bool `json:"RunSSH"`
		}
		if err := json.Unmarshal(out, &prefs); err == nil {
			return prefs.RunSSH
		}
	}
	return false
}

// HandleTailscale handles GET/POST /cgi-bin/quecmanager/vpn/tailscale.sh and /api/vpn/tailscale
func (h *TailscaleHandler) HandleTailscale(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.handleGetStatus(w, r)
		return
	}

	if r.Method == http.MethodPost {
		h.handlePostAction(w, r)
		return
	}

	Error(w, http.StatusMethodNotAllowed, "Method not allowed")
}

func (h *TailscaleHandler) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	installed := isTailscaleInstalled()
	if !installed {
		JSON(w, http.StatusOK, TailscaleStatus{
			Success:       true,
			Installed:     false,
			DaemonRunning: false,
			EnabledOnBoot: false,
			SSHEnabled:    false,
			BackendState:  "NoState",
			Peers:         []TailscalePeer{},
			Health:        []string{},
			InstallHint:   "sudo qmanager_tailscale_mgr install",
		})
		return
	}

	running := isTailscaleRunning()
	bootEnabled := isTailscaleBootEnabled()
	sshOn := isSSHEnabled()

	if !running {
		JSON(w, http.StatusOK, TailscaleStatus{
			Success:       true,
			Installed:     true,
			DaemonRunning: false,
			EnabledOnBoot: bootEnabled,
			SSHEnabled:    sshOn,
			BackendState:  "Stopped",
			Peers:         []TailscalePeer{},
			Health:        []string{},
		})
		return
	}

	out, err := h.statusExec()
	if err != nil {
		JSON(w, http.StatusOK, TailscaleStatus{
			Success:       true,
			Installed:     true,
			DaemonRunning: true,
			EnabledOnBoot: bootEnabled,
			SSHEnabled:    sshOn,
			BackendState:  "Starting",
			Peers:         []TailscalePeer{},
			Health:        []string{"Waiting for Tailscale socket..."},
		})
		return
	}

	var raw TailscaleRawStatus
	if err := json.Unmarshal(out, &raw); err != nil {
		JSON(w, http.StatusOK, TailscaleStatus{
			Success:       true,
			Installed:     true,
			DaemonRunning: true,
			EnabledOnBoot: bootEnabled,
			SSHEnabled:    sshOn,
			BackendState:  "Unknown",
			Peers:         []TailscalePeer{},
			Health:        []string{},
		})
		return
	}

	status := TailscaleStatus{
		Success:       true,
		Installed:     true,
		DaemonRunning: true,
		EnabledOnBoot: bootEnabled,
		SSHEnabled:    sshOn,
		Version:       raw.Version,
		BackendState:  raw.BackendState,
		AuthURL:       raw.AuthURL,
		Peers:         make([]TailscalePeer, 0),
		Health:        raw.Health,
	}
	if status.Health == nil {
		status.Health = make([]string, 0)
	}

	// Format Self
	ips := raw.Self.TailscaleIPs
	if ips == nil {
		ips = make([]string, 0)
	}
	status.Self = &TailscaleSelf{
		Hostname:     raw.Self.HostName,
		DNSName:      raw.Self.DNSName,
		TailscaleIPs: ips,
		Online:       raw.Self.Online,
		OS:           raw.Self.OS,
		Relay:        raw.Self.Relay,
	}

	// Format Tailnet
	if raw.CurrentTailnet != nil {
		status.Tailnet = &TailscaleTailnet{
			Name:            raw.CurrentTailnet.Name,
			MagicDNSSuffix:  raw.CurrentTailnet.MagicDNSSuffix,
			MagicDNSEnabled: raw.CurrentTailnet.MagicDNSEnabled,
		}
	} else if raw.MagicDNSSuffix != "" {
		status.Tailnet = &TailscaleTailnet{
			Name:            raw.MagicDNSSuffix,
			MagicDNSSuffix:  raw.MagicDNSSuffix,
			MagicDNSEnabled: true,
		}
	}

	// Format Peers
	for _, p := range raw.Peer {
		peerIPs := p.TailscaleIPs
		if peerIPs == nil {
			peerIPs = make([]string, 0)
		}
		status.Peers = append(status.Peers, TailscalePeer{
			Hostname:     p.HostName,
			DNSName:      p.DNSName,
			TailscaleIPs: peerIPs,
			OS:           p.OS,
			Online:       p.Online,
			LastSeen:     p.LastSeen,
			Relay:        p.Relay,
			ExitNode:     p.ExitNode,
		})
	}

	JSON(w, http.StatusOK, status)
}

func (h *TailscaleHandler) handlePostAction(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Action    string `json:"action"`
		AuthKey   string `json:"auth_key,omitempty"`
		Hostname  string `json:"hostname,omitempty"`
		EnableSSH bool   `json:"enable_ssh,omitempty"`
		Enabled   *bool  `json:"enabled,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "connect", "up":
		if !isTailscaleRunning() {
			_ = exec.Command("systemctl", "start", "tailscaled").Run()
			time.Sleep(1 * time.Second)
		}

		args := []string{"up", "--reset"}
		if payload.AuthKey != "" {
			args = append(args, "--authkey="+payload.AuthKey)
		}
		if payload.Hostname != "" {
			args = append(args, "--hostname="+payload.Hostname)
		}
		if payload.EnableSSH {
			args = append(args, "--ssh")
		}

		// Trigger tailscale up in a short-lived execution or goroutine
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = exec.CommandContext(ctx, "tailscale", args...).Run()
		}()

		// Give tailscale 1.5s to generate AuthURL if needed
		time.Sleep(1500 * time.Millisecond)

		out, err := h.statusExec()
		authURL := ""
		alreadyAuth := false
		if err == nil {
			var raw TailscaleRawStatus
			if json.Unmarshal(out, &raw) == nil {
				authURL = raw.AuthURL
				if raw.BackendState == "Running" {
					alreadyAuth = true
				}
			}
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"success":               true,
			"auth_url":              authURL,
			"already_authenticated": alreadyAuth,
			"message":               "Tailscale connecting",
		})

	case "disconnect", "down":
		_ = exec.Command("tailscale", "down").Run()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Tailscale disconnected",
		})

	case "logout":
		_ = exec.Command("tailscale", "logout").Run()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Tailscale logged out",
		})

	case "start_service":
		err := h.systemctlExec("start")
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to start tailscaled service")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Service started",
		})

	case "stop_service":
		err := h.systemctlExec("stop")
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to stop tailscaled service")
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Service stopped",
		})

	case "set_boot_enabled":
		enable := true
		if payload.Enabled != nil {
			enable = *payload.Enabled
		}
		action := "enable"
		if !enable {
			action = "disable"
		}
		_ = exec.Command("systemctl", action, "tailscaled").Run()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Service %sd on boot", action),
		})

	case "set_ssh":
		enable := true
		if payload.Enabled != nil {
			enable = *payload.Enabled
		}
		val := "true"
		if !enable {
			val = "false"
		}
		_ = exec.Command("tailscale", "set", fmt.Sprintf("--ssh=%s", val)).Run()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "SSH setting updated",
		})

	case "install":
		h.mu.Lock()
		if h.isInstalling {
			h.mu.Unlock()
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"status":  "running",
				"message": "Installation already in progress",
			})
			return
		}
		h.isInstalling = true
		h.mu.Unlock()

		go h.performInstallation()

		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "running",
			"message": "Installation started",
		})

	case "install_status":
		h.handleInstallStatus(w)

	case "uninstall":
		_ = exec.Command("systemctl", "stop", "tailscaled").Run()
		_ = exec.Command("systemctl", "disable", "tailscaled").Run()
		_ = os.Remove(tailscaleServiceUnit)
		_ = os.Remove("/usr/bin/tailscale")
		_ = os.RemoveAll(tailscaleDir)
		_ = exec.Command("systemctl", "daemon-reload").Run()
		_ = exec.Command("sync").Run()

		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Tailscale uninstalled successfully",
		})

	default:
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

func (h *TailscaleHandler) handleInstallStatus(w http.ResponseWriter) {
	data, err := os.ReadFile(tailscaleInstallJSON)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "idle",
		})
		return
	}

	var res map[string]interface{}
	if err := json.Unmarshal(data, &res); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "idle",
		})
		return
	}

	if logData, err := os.ReadFile(tailscaleInstallLog); err == nil {
		res["log"] = string(logData)
	}

	res["success"] = true
	JSON(w, http.StatusOK, res)
}

func writeInstallProgress(status, message, detail string) {
	entry := map[string]interface{}{
		"status":  status,
		"message": message,
		"detail":  detail,
		"time":    time.Now().Format(time.RFC3339),
	}
	bytes, _ := json.Marshal(entry)
	_ = os.WriteFile(tailscaleInstallJSON, bytes, 0644)

	logLine := fmt.Sprintf("[%s] %s: %s\n", time.Now().Format("15:04:05"), message, detail)
	f, err := os.OpenFile(tailscaleInstallLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(logLine)
		_ = f.Close()
	}
}

func (h *TailscaleHandler) performInstallation() {
	defer func() {
		h.mu.Lock()
		h.isInstalling = false
		h.mu.Unlock()
	}()

	_ = os.Remove(tailscaleInstallLog)
	writeInstallProgress("running", "Preparing installation", "Creating directories")

	if err := os.MkdirAll(tailscaleDir, 0755); err != nil {
		writeInstallProgress("error", "Failed to create directory", err.Error())
		return
	}

	url := fmt.Sprintf("https://pkgs.tailscale.com/stable/tailscale_%s_arm.tgz", tailscaleVersion)
	writeInstallProgress("running", "Downloading Tailscale", fmt.Sprintf("Fetching %s", url))

	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		writeInstallProgress("error", "Download failed", fmt.Sprintf("HTTP error: %v", err))
		return
	}
	defer resp.Body.Close()

	writeInstallProgress("running", "Extracting binaries", "Unpacking tar.gz archive")

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		writeInstallProgress("error", "Extraction failed", err.Error())
		return
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeInstallProgress("error", "Archive error", err.Error())
			return
		}

		baseName := filepath.Base(hdr.Name)
		if baseName == "tailscale" || baseName == "tailscaled" {
			targetPath := filepath.Join(tailscaleDir, baseName)
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				writeInstallProgress("error", "File write failed", err.Error())
				return
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				writeInstallProgress("error", "Binary extraction copy error", err.Error())
				return
			}
			outFile.Close()
			_ = os.Chmod(targetPath, 0755)
		}
	}

	writeInstallProgress("running", "Configuring system services", "Setting up systemd unit and symlinks")

	_ = os.Symlink(filepath.Join(tailscaleDir, "tailscale"), "/usr/bin/tailscale")
	_ = os.MkdirAll("/usrdata/root/bin", 0755)
	_ = os.Symlink(filepath.Join(tailscaleDir, "tailscale"), "/usrdata/root/bin/tailscale")

	serviceContent := `[Unit]
Description=Tailscale node agent
Documentation=https://tailscale.com/kb/
After=network.target

[Service]
ExecStartPre=/usrdata/tailscale/tailscaled --cleanup
ExecStart=/usrdata/tailscale/tailscaled --statedir=/usrdata/tailscale/ --port=41641
ExecStartPost=/bin/chmod 755 /usrdata/tailscale
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`
	_ = os.WriteFile(tailscaleServiceUnit, []byte(serviceContent), 0644)

	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "start", "tailscaled").Run()
	_ = exec.Command("sync").Run()

	writeInstallProgress("complete", "Installation successful", "Tailscale service is running")
}
