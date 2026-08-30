package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

const (
	apnSettingPath = "/etc/qmanager/apn_setting.json"
	apnNamesPath   = "/etc/qmanager/apn_names.json"
	maxApnProfiles = 6
)

// ApnSetting holds the single stored APN configuration.
type ApnSetting struct {
	Apn     string `json:"apn"`
	PdpType string `json:"pdp_type"`
	Cid     int    `json:"cid"`
	Active  int    `json:"active"`
}

// CidContext represents a live PDP context from the modem.
type CidContext struct {
	Cid        int    `json:"cid"`
	Apn        string `json:"apn"`
	ApnType    string `json:"apn_type"`
	IsInternet bool   `json:"is_internet"`
}

// ApnProfile represents a profile in the 6-slot view.
type ApnProfile struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Apn      string `json:"apn"`
	PdpType  string `json:"pdp_type"`
	AuthType string `json:"auth_type"`
	Username string `json:"username"`
	HasPw    int    `json:"has_password"`
	Enabled  int    `json:"enabled"`
	IsActive int    `json:"is_active"`
	ApnType  string `json:"apn_type"`
}

// CellularApnHandler handles APN management endpoints.
type CellularApnHandler struct {
	engine    *atengine.Engine
	configMgr *config.Manager
}

// NewCellularApnHandler creates a new CellularApnHandler.
func NewCellularApnHandler(engine *atengine.Engine, configMgr *config.Manager) *CellularApnHandler {
	return &CellularApnHandler{
		engine:    engine,
		configMgr: configMgr,
	}
}

// GetAPN handles GET /api/v1/cellular/apn and GET /cgi-bin/quecmanager/cellular/apn.sh
func (h *CellularApnHandler) GetAPN(w http.ResponseWriter, r *http.Request) {
	// 1. Read AT+CGDCONT?
	cgdResp, err := h.engine.Exec("AT+CGDCONT?")
	var cgdLines []string
	if err == nil {
		cgdLines = strings.Split(cgdResp.Raw, "\n")
	}

	// 2. Read AT+CGACT?
	cgactResp, _ := h.engine.Exec("AT+CGACT?")
	cgactLines := strings.Split(cgactResp.Raw, "\n")

	// 3. Read AT+CGCONTRDP
	contrdpResp, _ := h.engine.Exec("AT+CGCONTRDP")
	activeCid := parseActiveCidFromCGCONTRDP(contrdpResp.Raw)

	// 4. Read sidecars
	storedSetting := readApnSetting()
	namesMap := readApnNames()

	profiles := make([]ApnProfile, 0, maxApnProfiles)
	cids := make([]CidContext, 0, maxApnProfiles)

	for cid := 1; cid <= maxApnProfiles; cid++ {
		pdpRaw, apn := parseCGDCONTLine(cgdLines, cid)
		pdpType := pdpToFrontend(pdpRaw)
		apnType := classifyApnType(apn)
		enabled := getCgactState(cgactLines, cid)

		isActive := 0
		if cid == activeCid {
			isActive = 1
		}

		cids = append(cids, CidContext{
			Cid:        cid,
			Apn:        apn,
			ApnType:    apnType,
			IsInternet: cid == activeCid,
		})

		name := namesMap[strconv.Itoa(cid)]
		profiles = append(profiles, ApnProfile{
			Index:    cid,
			Name:     name,
			Apn:      apn,
			PdpType:  pdpType,
			AuthType: "none",
			Username: "",
			HasPw:    0,
			Enabled:  enabled,
			IsActive: isActive,
			ApnType:  apnType,
		})
	}

	internetCid := activeCid
	if internetCid == 0 {
		internetCid = 1
	}

	resp := map[string]interface{}{
		"success":      true,
		"profiles":     profiles,
		"active":       storedSetting.Active,
		"active_cid":   internetCid,
		"internet_cid": internetCid,
		"apn": map[string]interface{}{
			"apn":      storedSetting.Apn,
			"pdp_type": storedSetting.PdpType,
			"cid":      storedSetting.Cid,
		},
		"cids": cids,
	}

	JSON(w, http.StatusOK, resp)
}

// ApnSavePayload handles the polymorphic POST request for save/deactivate/toggle.
type ApnSavePayload struct {
	Action   string      `json:"action"`
	Index    interface{} `json:"index,omitempty"`
	Cid      interface{} `json:"cid,omitempty"`
	Apn      *string     `json:"apn,omitempty"`
	PdpType  *string     `json:"pdp_type,omitempty"`
	AuthType *string     `json:"auth_type,omitempty"`
	Username *string     `json:"username,omitempty"`
	Password *string     `json:"password,omitempty"`
	Name     *string     `json:"name,omitempty"`
	Enabled  *bool       `json:"enabled,omitempty"`
}

// SaveAPN handles POST /api/v1/cellular/apn and POST /cgi-bin/quecmanager/cellular/apn.sh
func (h *CellularApnHandler) SaveAPN(w http.ResponseWriter, r *http.Request) {
	var payload ApnSavePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	switch payload.Action {
	case "deactivate":
		h.handleDeactivate(w)
	case "toggle":
		h.handleToggle(w, payload)
	case "save":
		h.handleSave(w, payload)
	default:
		Error(w, http.StatusBadRequest, "Unknown action")
	}
}

func (h *CellularApnHandler) handleDeactivate(w http.ResponseWriter) {
	setting := readApnSetting()
	if setting.Active != 1 {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"active":  0,
		})
		return
	}

	cid := setting.Cid
	if cid < 1 || cid > maxApnProfiles {
		cid = 1
	}
	pdpAT := pdpToAT(setting.PdpType)
	if pdpAT == "" {
		pdpAT = "IPV4V6"
	}

	// Write empty APN to revert to carrier default
	err := h.applyApnAttachCycle(cid, pdpAT, "", true)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	setting.Active = 0
	setting.Apn = ""
	_ = writeApnSetting(setting)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  0,
	})
}

func (h *CellularApnHandler) handleToggle(w http.ResponseWriter, p ApnSavePayload) {
	cid := parseCID(p.Index, p.Cid)
	if cid < 1 || cid > maxApnProfiles {
		Error(w, http.StatusBadRequest, "Invalid index/cid")
		return
	}
	if p.Enabled == nil {
		Error(w, http.StatusBadRequest, "enabled field required")
		return
	}

	enVal := 0
	if *p.Enabled {
		enVal = 1
	}

	cmd := fmt.Sprintf("AT+CGACT=%d,%d", enVal, cid)
	if _, err := h.engine.Exec(cmd); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("AT+CGACT failed: %v", err))
		return
	}

	Success(w, map[string]interface{}{"message": "Toggled successfully"})
}

func (h *CellularApnHandler) handleSave(w http.ResponseWriter, p ApnSavePayload) {
	cid := parseCID(p.Index, p.Cid)
	if cid < 1 || cid > maxApnProfiles {
		cid = 1
	}

	// Determine if WS6 single-APN or legacy
	isSingleAPN := p.Index == nil

	apn := ""
	if p.Apn != nil {
		apn = *p.Apn
	}
	if apn == "" {
		Error(w, http.StatusBadRequest, "apn is required")
		return
	}
	if strings.Contains(apn, "\"") {
		Error(w, http.StatusBadRequest, "APN may not contain double-quotes")
		return
	}

	pdpType := "ipv4v6"
	if p.PdpType != nil && *p.PdpType != "" {
		pdpType = *p.PdpType
	}
	pdpAT := pdpToAT(pdpType)
	if pdpAT == "" {
		Error(w, http.StatusBadRequest, "Invalid pdp_type (must be ipv4, ipv6, or ipv4v6)")
		return
	}

	if !isSingleAPN {
		// Legacy save also handles auth & name
		if p.Name != nil && *p.Name != "" {
			names := readApnNames()
			names[strconv.Itoa(cid)] = *p.Name
			_ = writeApnNames(names)
		}
		if p.AuthType != nil {
			authVal := authToAT(*p.AuthType)
			user := ""
			if p.Username != nil {
				user = *p.Username
			}
			pass := ""
			if p.Password != nil {
				pass = *p.Password
			}
			qicsgpCmd := fmt.Sprintf(`AT+QICSGP=%d,%s,"%s","%s","%s",%s`, cid, pdpToCtxtype(pdpType), apn, user, pass, authVal)
			_, _ = h.engine.Exec(qicsgpCmd)
		}
	}

	// Run write-first attach cycle
	err := h.applyApnAttachCycle(cid, pdpAT, apn, false)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist to APN setting sidecar
	setting := ApnSetting{
		Apn:     apn,
		PdpType: pdpType,
		Cid:     cid,
		Active:  1,
	}
	_ = writeApnSetting(setting)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"active":  1,
	})
}

// applyApnAttachCycle executes the canonical write-first attach cycle.
func (h *CellularApnHandler) applyApnAttachCycle(cid int, pdpType, apn string, allowEmpty bool) error {
	// 1. Write AT+CGDCONT
	cgdCmd := fmt.Sprintf(`AT+CGDCONT=%d,"%s","%s"`, cid, pdpType, apn)
	res, err := h.engine.Exec(cgdCmd)
	if err != nil || !strings.Contains(res.Raw, "OK") {
		return fmt.Errorf("cgdcont_failed: AT+CGDCONT failed for CID %d", cid)
	}

	// Sleep 1s before detach
	time.Sleep(1 * time.Second)

	// 2. Detach AT+COPS=2
	res, err = h.engine.Exec("AT+COPS=2")
	if err != nil || !strings.Contains(res.Raw, "OK") {
		return fmt.Errorf("cops_detach_failed: AT+COPS=2 failed")
	}

	time.Sleep(1 * time.Second)

	// 3. Re-attach AT+COPS=0 with retries
	attached := false
	for attempt := 1; attempt <= 3; attempt++ {
		res, err = h.engine.Exec("AT+COPS=0")
		if err == nil && strings.Contains(res.Raw, "OK") {
			attached = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !attached {
		return fmt.Errorf("cops_attach_failed: AT+COPS=0 failed after 3 attempts")
	}

	if allowEmpty && apn == "" {
		return nil
	}

	// 4. Verify with AT+CGCONTRDP (wait up to 10s for bearer negotiation)
	var negotiatedApn string
	for attempt := 1; attempt <= 5; attempt++ {
		time.Sleep(2 * time.Second)
		res, err = h.engine.Exec("AT+CGCONTRDP")
		if err == nil {
			negotiatedApn = parseNegotiatedApnForCid(res.Raw, cid)
			if negotiatedApn != "" {
				break
			}
		}
	}

	if negotiatedApn != "" && !strings.EqualFold(negotiatedApn, apn) {
		// Log warning or return mismatch error
		return fmt.Errorf("apn_mismatch: Requested %s, network negotiated %s", apn, negotiatedApn)
	}

	return nil
}

// Helpers

func parseCID(index, cid interface{}) int {
	if cid != nil {
		if c, ok := cid.(float64); ok {
			return int(c)
		}
		if s, ok := cid.(string); ok {
			if i, err := strconv.Atoi(s); err == nil {
				return i
			}
		}
	}
	if index != nil {
		if idx, ok := index.(float64); ok {
			return int(idx)
		}
		if s, ok := index.(string); ok {
			if i, err := strconv.Atoi(s); err == nil {
				return i
			}
		}
	}
	return 0
}

func pdpToFrontend(raw string) string {
	switch strings.ToUpper(raw) {
	case "IP":
		return "ipv4"
	case "IPV6":
		return "ipv6"
	default:
		return "ipv4v6"
	}
}

func pdpToAT(pdp string) string {
	switch strings.ToLower(pdp) {
	case "ipv4":
		return "IP"
	case "ipv6":
		return "IPV6"
	case "ipv4v6":
		return "IPV4V6"
	default:
		return ""
	}
}

func pdpToCtxtype(pdp string) string {
	switch strings.ToLower(pdp) {
	case "ipv4":
		return "1"
	case "ipv6":
		return "2"
	default:
		return "3"
	}
}

func authToAT(auth string) string {
	switch strings.ToLower(auth) {
	case "pap":
		return "1"
	case "chap":
		return "2"
	default:
		return "0"
	}
}

func classifyApnType(apn string) string {
	switch strings.ToLower(apn) {
	case "ims":
		return "ims"
	case "sos":
		return "emergency"
	default:
		return ""
	}
}

func parseCGDCONTLine(lines []string, targetCid int) (pdpRaw, apn string) {
	prefix := fmt.Sprintf("+CGDCONT: %d,", targetCid)
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, prefix) {
			parts := strings.Split(l, ",")
			if len(parts) >= 2 {
				pdpRaw = strings.Trim(parts[1], "\" ")
			}
			if len(parts) >= 3 {
				apn = strings.Trim(parts[2], "\" ")
			}
			return
		}
	}
	return "", ""
}

func getCgactState(lines []string, targetCid int) int {
	prefix := fmt.Sprintf("+CGACT: %d,", targetCid)
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, prefix) {
			parts := strings.Split(l, ",")
			if len(parts) >= 2 {
				st := strings.TrimSpace(parts[1])
				if st == "1" {
					return 1
				}
			}
			return 0
		}
		// In some firmwares: +CGACT: <cid>,<state> or +CGACT: <state>,<cid>
		if strings.HasPrefix(l, "+CGACT:") {
			trimmed := strings.TrimPrefix(l, "+CGACT:")
			parts := strings.Split(trimmed, ",")
			if len(parts) >= 2 {
				c, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
				s, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				if c == targetCid && s == 1 {
					return 1
				}
			}
		}
	}
	return 0
}

func parseActiveCidFromCGCONTRDP(raw string) int {
	// +CGCONTRDP: <cid>,...
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CGCONTRDP:") {
			trimmed := strings.TrimPrefix(l, "+CGCONTRDP:")
			parts := strings.Split(trimmed, ",")
			if len(parts) > 0 {
				cidStr := strings.TrimSpace(parts[0])
				if cid, err := strconv.Atoi(cidStr); err == nil && cid > 0 {
					return cid
				}
			}
		}
	}
	return 1
}

func parseNegotiatedApnForCid(raw string, targetCid int) string {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CGCONTRDP:") {
			trimmed := strings.TrimPrefix(l, "+CGCONTRDP:")
			parts := strings.Split(trimmed, ",")
			if len(parts) >= 3 {
				cidStr := strings.TrimSpace(parts[0])
				cid, err := strconv.Atoi(cidStr)
				if err == nil && cid == targetCid {
					return strings.Trim(parts[2], "\" ")
				}
			}
		}
	}
	return ""
}

func readApnSetting() ApnSetting {
	data, err := os.ReadFile(apnSettingPath)
	if err != nil {
		return ApnSetting{Active: 0, Cid: 1, PdpType: "ipv4v6"}
	}
	var s ApnSetting
	if err := json.Unmarshal(data, &s); err != nil {
		return ApnSetting{Active: 0, Cid: 1, PdpType: "ipv4v6"}
	}
	if s.Cid == 0 {
		s.Cid = 1
	}
	if s.PdpType == "" {
		s.PdpType = "ipv4v6"
	}
	return s
}

func writeApnSetting(s ApnSetting) error {
	_ = os.MkdirAll(filepath.Dir(apnSettingPath), 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(apnSettingPath, data, 0644)
}

func readApnNames() map[string]string {
	data, err := os.ReadFile(apnNamesPath)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func writeApnNames(m map[string]string) error {
	_ = os.MkdirAll(filepath.Dir(apnNamesPath), 0755)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(apnNamesPath, data, 0644)
}
