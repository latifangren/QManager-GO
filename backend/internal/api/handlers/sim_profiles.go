package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"qmanager/internal/atengine"
)

const (
	maxProfiles = 10
)

var (
	defaultProfileDir        = "/etc/qmanager/profiles"
	defaultActiveProfilePath = "/etc/qmanager/active_profile"
	defaultProfileStatePath  = "/tmp/qmanager_profile_state.json"
	defaultSimRegistryPath   = "/etc/qmanager/sim_registry.json"
)

// ProfileSettings represents core radio/network settings in a profile.
type ProfileSettings struct {
	APN          string `json:"apn"`
	PDPType      string `json:"pdp_type"`
	AuthType     string `json:"auth_type"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	IMEI         string `json:"imei,omitempty"`
	TTL          *int   `json:"ttl,omitempty"`
	HL           *int   `json:"hl,omitempty"`
	CID          int    `json:"cid"`
	AutoConnect  bool   `json:"auto_connect"`
	Roaming      bool   `json:"roaming"`
	ScenarioID   string `json:"scenario_id,omitempty"` // legacy fallback
}

// ScenarioBlock represents scenario scheduling within a profile.
type ScenarioBlock struct {
	Default  string `json:"default"`
	Schedule struct {
		Enabled bool `json:"enabled"`
		Blocks  []struct {
			Days       []int  `json:"days"`
			StartTime  string `json:"start_time"`
			EndTime    string `json:"end_time"`
			ScenarioID string `json:"scenario_id"`
		} `json:"blocks"`
	} `json:"schedule"`
}

// SIMProfile represents a full SIM profile definition.
type SIMProfile struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ICCID     string          `json:"iccid,omitempty"`
	Settings  ProfileSettings `json:"settings"`
	Scenario  *ScenarioBlock  `json:"scenario,omitempty"`
	CreatedAt int64           `json:"created_at,omitempty"`
	UpdatedAt int64           `json:"updated_at,omitempty"`
}

// ProfileSummary is a lightweight view for profile lists.
type ProfileSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ICCID     string `json:"iccid,omitempty"`
	APN       string `json:"apn"`
	PDPType   string `json:"pdp_type"`
	CID       int    `json:"cid"`
	IsActive  bool   `json:"is_active"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// ProfileApplyState represents async profile application state.
type ProfileApplyState struct {
	Status    string   `json:"status"` // "idle", "applying", "complete", "failed"
	ProfileID string   `json:"profile_id,omitempty"`
	Step      string   `json:"step,omitempty"`
	Progress  int      `json:"progress,omitempty"`
	Error     string   `json:"error,omitempty"`
	Log       []string `json:"log,omitempty"`
	Timestamp int64    `json:"timestamp"`
}

// SIMProfileHandler handles profile CRUD, activation, and status queries.
type SIMProfileHandler struct {
	engine            *atengine.Engine
	profileDir        string
	activeProfilePath string
	profileStatePath  string
	mu                sync.Mutex
	wg                sync.WaitGroup
}

// NewSIMProfileHandler creates a new SIMProfileHandler.
func NewSIMProfileHandler(engine *atengine.Engine) *SIMProfileHandler {
	return &SIMProfileHandler{
		engine:            engine,
		profileDir:        defaultProfileDir,
		activeProfilePath: defaultActiveProfilePath,
		profileStatePath:  defaultProfileStatePath,
	}
}

// SetStoragePaths allows customizing paths for isolated unit tests.
func (h *SIMProfileHandler) SetStoragePaths(profileDir, activePath, statePath string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.profileDir = profileDir
	h.activeProfilePath = activePath
	h.profileStatePath = statePath
}

// WaitForAsync blocks until any running background tasks complete.
func (h *SIMProfileHandler) WaitForAsync() {
	h.wg.Wait()
}

func (h *SIMProfileHandler) getActiveProfileID() string {
	data, err := os.ReadFile(h.activeProfilePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (h *SIMProfileHandler) setActiveProfileID(id string) error {
	_ = os.MkdirAll(filepath.Dir(h.activeProfilePath), 0755)
	if id == "" {
		_ = os.Remove(h.activeProfilePath)
		return nil
	}
	return os.WriteFile(h.activeProfilePath, []byte(id), 0644)
}

func generateProfileID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	suffix := hex.EncodeToString(b)[:3]
	return fmt.Sprintf("p_%d_%s", time.Now().Unix(), suffix)
}

// List handles GET /api/v1/cellular/profiles and GET /cgi-bin/quecmanager/profiles/list.sh
func (h *SIMProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	activeID := h.getActiveProfileID()
	var summaries []ProfileSummary

	_ = os.MkdirAll(h.profileDir, 0755)
	entries, err := os.ReadDir(h.profileDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(h.profileDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var p SIMProfile
			if err := json.Unmarshal(data, &p); err != nil {
				continue
			}
			summaries = append(summaries, ProfileSummary{
				ID:        p.ID,
				Name:      p.Name,
				ICCID:     p.ICCID,
				APN:       p.Settings.APN,
				PDPType:   p.Settings.PDPType,
				CID:       p.Settings.CID,
				IsActive:  p.ID == activeID,
				CreatedAt: p.CreatedAt,
				UpdatedAt: p.UpdatedAt,
			})
		}
	}

	if summaries == nil {
		summaries = []ProfileSummary{}
	}

	var activeVal interface{} = activeID
	if activeID == "" {
		activeVal = nil
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"profiles":          summaries,
		"active_profile_id": activeVal,
	})
}

// Get handles GET /api/v1/cellular/profiles/{id} and GET /cgi-bin/quecmanager/profiles/get.sh
func (h *SIMProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if id == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 && parts[len(parts)-1] != "profiles" && !strings.HasSuffix(parts[len(parts)-1], ".sh") {
			id = parts[len(parts)-1]
		}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Error(w, http.StatusBadRequest, "Missing profile ID")
		return
	}

	h.mu.Lock()
	path := filepath.Join(h.profileDir, id+".json")
	data, err := os.ReadFile(path)
	h.mu.Unlock()

	if err != nil {
		Error(w, http.StatusNotFound, "Profile not found")
		return
	}

	var p SIMProfile
	if err := json.Unmarshal(data, &p); err != nil {
		Error(w, http.StatusInternalServerError, "Malformed profile file")
		return
	}

	JSON(w, http.StatusOK, p)
}

// Save handles POST /api/v1/cellular/profiles and POST /cgi-bin/quecmanager/profiles/save.sh
func (h *SIMProfileHandler) Save(w http.ResponseWriter, r *http.Request) {
	var p SIMProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		Error(w, http.StatusBadRequest, "Invalid profile payload")
		return
	}

	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		Error(w, http.StatusBadRequest, "Profile name is required")
		return
	}

	if p.Settings.APN == "" {
		Error(w, http.StatusBadRequest, "APN is required")
		return
	}

	if p.Settings.PDPType == "" {
		p.Settings.PDPType = "IPV4V6"
	}
	p.Settings.PDPType = strings.ToUpper(p.Settings.PDPType)
	if p.Settings.PDPType != "IP" && p.Settings.PDPType != "IPV6" && p.Settings.PDPType != "IPV4V6" {
		Error(w, http.StatusBadRequest, "Invalid pdp_type (IP, IPV6, IPV4V6 expected)")
		return
	}

	if p.Settings.CID <= 0 {
		p.Settings.CID = 1
	}

	if p.Settings.IMEI != "" {
		p.Settings.IMEI = strings.TrimSpace(p.Settings.IMEI)
		if len(p.Settings.IMEI) != 15 || !isNumeric(p.Settings.IMEI) {
			Error(w, http.StatusBadRequest, "IMEI must be 15 digits")
			return
		}
	}

	if p.Settings.TTL != nil && (*p.Settings.TTL < 0 || *p.Settings.TTL > 255) {
		Error(w, http.StatusBadRequest, "TTL must be between 0 and 255")
		return
	}
	if p.Settings.HL != nil && (*p.Settings.HL < 0 || *p.Settings.HL > 255) {
		Error(w, http.StatusBadRequest, "HL must be between 0 and 255")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_ = os.MkdirAll(h.profileDir, 0755)

	isNew := p.ID == ""
	if isNew {
		// Check profile count limit
		entries, _ := os.ReadDir(h.profileDir)
		count := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				count++
			}
		}
		if count >= maxProfiles {
			Error(w, http.StatusBadRequest, fmt.Sprintf("Maximum profile count (%d) reached", maxProfiles))
			return
		}
		p.ID = generateProfileID()
		p.CreatedAt = time.Now().Unix()
	}

	p.UpdatedAt = time.Now().Unix()
	if p.CreatedAt == 0 {
		p.CreatedAt = p.UpdatedAt
	}

	path := filepath.Join(h.profileDir, p.ID+".json")
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to encode profile JSON")
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save profile file")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      p.ID,
		"profile": p,
		"message": "Profile saved successfully",
	})
}

// Delete handles DELETE /api/v1/cellular/profiles/{id} and POST /cgi-bin/quecmanager/profiles/delete.sh
func (h *SIMProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if id == "" && r.Method == http.MethodPost {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 && parts[len(parts)-1] != "profiles" && !strings.HasSuffix(parts[len(parts)-1], ".sh") {
			id = parts[len(parts)-1]
		}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Error(w, http.StatusBadRequest, "Missing profile ID")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	path := filepath.Join(h.profileDir, id+".json")
	if _, err := os.Stat(path); err != nil {
		Error(w, http.StatusNotFound, "Profile not found")
		return
	}

	_ = os.Remove(path)

	// If active profile was deleted, clear active marker
	if h.getActiveProfileID() == id {
		_ = h.setActiveProfileID("")
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Profile deleted",
	})
}

// Apply handles POST /api/v1/cellular/profiles/apply and POST /cgi-bin/quecmanager/profiles/apply.sh
func (h *SIMProfileHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProfileID string `json:"profile_id"`
		ID        string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	id := req.ProfileID
	if id == "" {
		id = req.ID
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Error(w, http.StatusBadRequest, "Missing profile_id")
		return
	}

	h.mu.Lock()
	path := filepath.Join(h.profileDir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		h.mu.Unlock()
		Error(w, http.StatusNotFound, "Profile not found")
		return
	}

	var p SIMProfile
	_ = json.Unmarshal(data, &p)
	_ = h.setActiveProfileID(id)

	// Record initial apply state
	state := ProfileApplyState{
		Status:    "applying",
		ProfileID: id,
		Step:      "configuring_pdp",
		Progress:  20,
		Timestamp: time.Now().Unix(),
		Log:       []string{fmt.Sprintf("Started applying profile %s (%s)", p.Name, id)},
	}
	_ = h.writeState(state)
	h.mu.Unlock()

	// Execute apply steps asynchronously or synchronously via AT engine
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.mu.Lock()
		defer h.mu.Unlock()

		// Configure APN on modem: AT+CGDCONT=<cid>,"<pdp_type>","<apn>"
		cgdCmd := fmt.Sprintf(`AT+CGDCONT=%d,"%s","%s"`, p.Settings.CID, p.Settings.PDPType, p.Settings.APN)
		_, _ = h.engine.Exec(cgdCmd)

		if p.Settings.AuthType != "" && p.Settings.Username != "" {
			authVal := 0
			if strings.EqualFold(p.Settings.AuthType, "PAP") {
				authVal = 1
			} else if strings.EqualFold(p.Settings.AuthType, "CHAP") {
				authVal = 2
			} else if strings.EqualFold(p.Settings.AuthType, "PAP_CHAP") {
				authVal = 3
			}
			authCmd := fmt.Sprintf(`AT+QICSGP=%d,%d,"%s","%s",%d`, p.Settings.CID, authVal, p.Settings.Username, p.Settings.Password, authVal)
			_, _ = h.engine.Exec(authCmd)
		}

		// Configure TTL if specified
		if p.Settings.TTL != nil {
			_ = exec.Command("iptables", "-t", "mangle", "-D", "POSTROUTING", "-o", "rmnet+", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", *p.Settings.TTL)).Run()
			_ = exec.Command("iptables", "-t", "mangle", "-A", "POSTROUTING", "-o", "rmnet+", "-j", "TTL", "--ttl-set", fmt.Sprintf("%d", *p.Settings.TTL)).Run()
		}

		state.Status = "complete"
		state.Progress = 100
		state.Step = "done"
		state.Timestamp = time.Now().Unix()
		state.Log = append(state.Log, "Profile application completed successfully")
		_ = h.writeState(state)
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"status":     "started",
		"profile_id": id,
		"message":    "Profile application initiated",
	})
}

// ApplyStatus handles GET /api/v1/cellular/profiles/apply-status and GET /cgi-bin/quecmanager/profiles/apply_status.sh
func (h *SIMProfileHandler) ApplyStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := os.ReadFile(h.profileStatePath)
	if err != nil {
		JSON(w, http.StatusOK, ProfileApplyState{
			Status:    "idle",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	var state ProfileApplyState
	if err := json.Unmarshal(data, &state); err != nil {
		JSON(w, http.StatusOK, ProfileApplyState{
			Status:    "idle",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	JSON(w, http.StatusOK, state)
}

// Deactivate handles POST /cgi-bin/quecmanager/profiles/deactivate.sh and resets the active SIM profile.
func (h *SIMProfileHandler) Deactivate(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	_ = h.setActiveProfileID("")
	state := ProfileApplyState{
		Status:    "idle",
		Timestamp: time.Now().Unix(),
	}
	_ = h.writeState(state)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Active profile deactivated",
	})
}

func (h *SIMProfileHandler) writeState(st ProfileApplyState) error {
	_ = os.MkdirAll(filepath.Dir(h.profileStatePath), 0755)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.profileStatePath, data, 0644)
}

// CurrentSettings handles GET /api/v1/cellular/profiles/current-settings and GET /cgi-bin/quecmanager/profiles/current_settings.sh
func (h *SIMProfileHandler) CurrentSettings(w http.ResponseWriter, r *http.Request) {
	// Query compound AT commands: AT+CGDCONT?;+CGSN;+QCCID;+CGPADDR;+QMAP="WWAN";+QSPN
	rawResp, _ := h.engine.Exec(`AT+CGDCONT?;+CGSN;+QCCID;+CGPADDR;+QMAP="WWAN";+QSPN`)
	raw := ""
	if rawResp != nil {
		raw = rawResp.Raw
	}

	settings := ParseCurrentSettings(raw)
	JSON(w, http.StatusOK, settings)
}

// CurrentModemSettings holds live modem settings for the profile create form.
type CurrentModemSettings struct {
	APNProfiles []CidContext `json:"apn_profiles"`
	IMEI        string       `json:"imei"`
	ICCID       string       `json:"iccid"`
	ActiveCID   int          `json:"active_cid"`
	SPN         string       `json:"spn"`
	NetworkName string       `json:"network_name"`
	MCC         string       `json:"mcc"`
	MNC         string       `json:"mnc"`
}

// ParseCurrentSettings extracts live modem values from AT compound queries.
func ParseCurrentSettings(raw string) CurrentModemSettings {
	res := CurrentModemSettings{
		APNProfiles: []CidContext{},
		ActiveCID:   1,
	}

	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		line := strings.TrimSpace(l)

		// 1. +CGDCONT: <cid>,"<type>","<apn>",...
		if strings.HasPrefix(line, "+CGDCONT:") {
			idx := strings.Index(line, "+CGDCONT:")
			rest := strings.TrimSpace(line[idx+len("+CGDCONT:"):])
			parts := strings.Split(rest, ",")
			if len(parts) >= 3 {
				cid, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
				pdpType := strings.Trim(strings.TrimSpace(parts[1]), `"`)
				apn := strings.Trim(strings.TrimSpace(parts[2]), `"`)
				if cid > 0 {
					res.APNProfiles = append(res.APNProfiles, CidContext{
						Cid:        cid,
						Apn:        apn,
						ApnType:    pdpType,
						IsInternet: cid == 1,
					})
				}
			}
		}

		// 2. IMEI from AT+CGSN (bare 15-digit line)
		if len(line) == 15 && isNumeric(line) {
			res.IMEI = line
		}

		// 3. ICCID from +QCCID: <iccid>
		if strings.HasPrefix(line, "+QCCID:") {
			iccid := strings.TrimSpace(strings.TrimPrefix(line, "+QCCID:"))
			// Canonicalize trailing 'F'
			iccid = strings.TrimSuffix(strings.TrimSuffix(iccid, "F"), "f")
			res.ICCID = iccid
		}

		// 4. SPN & Network Name from +QSPN: "<FNN>","<SNN>","<SPN>",<disp>,"<RPLMN>"
		if strings.HasPrefix(line, "+QSPN:") {
			tokens := strings.Split(line, `"`)
			if len(tokens) >= 7 {
				res.NetworkName = tokens[1] // FNN
				res.SPN = tokens[5]         // SPN
				plmn := tokens[len(tokens)-2]
				if len(plmn) >= 4 && isNumeric(plmn) {
					res.MCC = plmn[:3]
					res.MNC = plmn[3:]
				}
			}
		}

		// 5. Active CID from +QMAP: ... or +CGPADDR:
		if strings.HasPrefix(line, "+QMAP:") {
			parts := strings.Split(line, ",")
			if len(parts) >= 5 {
				cid, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
				ip := strings.Trim(strings.TrimSpace(parts[4]), `"`)
				if ip != "" && ip != "0.0.0.0" && cid > 0 {
					res.ActiveCID = cid
				}
			}
		}
	}

	return res
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
