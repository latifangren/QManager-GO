package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"qmanager/internal/atengine"
)

// MbnProfile matches the frontend TypeScript interface.
type MbnProfile struct {
	Index     int    `json:"index"`
	Selected  bool   `json:"selected"`
	Activated bool   `json:"activated"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Date      string `json:"date"`
}

// CellularMbnHandler handles MBN configuration and profiles via AT+QMBNCFG.
type CellularMbnHandler struct {
	engine *atengine.Engine
}

// NewCellularMbnHandler creates a CellularMbnHandler.
func NewCellularMbnHandler(engine *atengine.Engine) *CellularMbnHandler {
	return &CellularMbnHandler{engine: engine}
}

// GetMBN handles GET /api/v1/cellular/mbn and GET /cgi-bin/quecmanager/cellular/mbn.sh
func (h *CellularMbnHandler) GetMBN(w http.ResponseWriter, r *http.Request) {
	// Read auto-select status: AT+QMBNCFG="autosel"
	autoSel := 0
	resAuto, err := h.engine.Exec(`AT+QMBNCFG="autosel"`)
	if err == nil {
		autoSel = parseMbnAutoSel(resAuto.Raw)
	}

	// Read profile list: AT+QMBNCFG="list"
	resList, err := h.engine.Exec(`AT+QMBNCFG="list"`)
	var profiles []MbnProfile
	if err == nil {
		profiles = ParseMbnList(resList.Raw)
	} else {
		profiles = []MbnProfile{}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"auto_sel": autoSel,
		"profiles": profiles,
	})
}

// MbnSavePayload represents the POST request body.
type MbnSavePayload struct {
	Action      string `json:"action"`
	ProfileName string `json:"profile_name,omitempty"`
	AutoSel     *int   `json:"auto_sel,omitempty"`
}

// SaveMBN handles POST /api/v1/cellular/mbn and POST /cgi-bin/quecmanager/cellular/mbn.sh
func (h *CellularMbnHandler) SaveMBN(w http.ResponseWriter, r *http.Request) {
	var p MbnSavePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch p.Action {
	case "apply_profile":
		h.handleApplyProfile(w, p.ProfileName)
	case "auto_sel":
		h.handleAutoSel(w, p.AutoSel)
	case "reboot":
		h.handleReboot(w)
	default:
		Error(w, http.StatusBadRequest, "Unknown action")
	}
}

func (h *CellularMbnHandler) handleApplyProfile(w http.ResponseWriter, profileName string) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		Error(w, http.StatusBadRequest, "profile_name is required")
		return
	}

	// 1. Select profile: AT+QMBNCFG="select","<name>"
	cmdSelect := fmt.Sprintf(`AT+QMBNCFG="select","%s"`, profileName)
	res, err := h.engine.Exec(cmdSelect)
	if err != nil || !strings.Contains(res.Raw, "OK") {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to select MBN profile: %v", err))
		return
	}

	// 2. Disable autosel if manual profile selected: AT+QMBNCFG="autosel",0
	_, _ = h.engine.Exec(`AT+QMBNCFG="autosel",0`)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"detail":          fmt.Sprintf("MBN profile '%s' selected. Reboot required to activate.", profileName),
		"reboot_required": true,
	})
}

func (h *CellularMbnHandler) handleAutoSel(w http.ResponseWriter, autoSel *int) {
	if autoSel == nil || (*autoSel != 0 && *autoSel != 1) {
		Error(w, http.StatusBadRequest, "auto_sel must be 0 or 1")
		return
	}

	cmd := fmt.Sprintf(`AT+QMBNCFG="autosel",%d`, *autoSel)
	res, err := h.engine.Exec(cmd)
	if err != nil || !strings.Contains(res.Raw, "OK") {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set MBN autosel: %v", err))
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"detail":          fmt.Sprintf("MBN auto-select set to %d. Reboot recommended.", *autoSel),
		"reboot_required": true,
	})
}

func (h *CellularMbnHandler) handleReboot(w http.ResponseWriter) {
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("reboot").Run()
	}()

	Success(w, map[string]interface{}{
		"message": "Rebooting device...",
	})
}

// ParseMbnList parses the output of AT+QMBNCFG="list".
// Hardware format:
// +QMBNCFG: "List",<index>,<selected>,<activated>,"<name>",<version_hex>,<date>
func ParseMbnList(raw string) []MbnProfile {
	var profiles []MbnProfile
	lines := strings.Split(raw, "\n")

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "+QMBNCFG:") || !strings.Contains(l, `"List"`) {
			continue
		}

		// Split line carefully
		// Example: +QMBNCFG: "List",0,1,1,"ROW_Commercial",0x08010801,202305091
		trimmed := strings.TrimPrefix(l, "+QMBNCFG:")
		parts := strings.Split(trimmed, ",")
		if len(parts) < 7 {
			continue
		}

		idx, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		selected := strings.TrimSpace(parts[2]) == "1"
		activated := strings.TrimSpace(parts[3]) == "1"
		name := strings.Trim(parts[4], "\" ")
		version := strings.TrimSpace(parts[5])
		date := strings.TrimSpace(parts[6])

		profiles = append(profiles, MbnProfile{
			Index:     idx,
			Selected:  selected,
			Activated: activated,
			Name:      name,
			Version:   version,
			Date:      date,
		})
	}

	return profiles
}

func parseMbnAutoSel(raw string) int {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+QMBNCFG:") && strings.Contains(l, "AutoSel") {
			parts := strings.Split(l, ",")
			if len(parts) >= 2 {
				val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				return val
			}
		}
	}
	return 0
}
