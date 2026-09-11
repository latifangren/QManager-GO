package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"qmanager/internal/atengine"
	"qmanager/internal/telemetry"
)

// CellularHandler handles cellular telemetry, AT queries, and locks.
type CellularHandler struct {
	engine   *atengine.Engine
	poller   *telemetry.Poller
	failover *BandFailoverHandler
}

// NewCellularHandler creates a CellularHandler.
func NewCellularHandler(eng *atengine.Engine, poller *telemetry.Poller, fo ...*BandFailoverHandler) *CellularHandler {
	var failoverH *BandFailoverHandler
	if len(fo) > 0 {
		failoverH = fo[0]
	}
	return &CellularHandler{
		engine:   eng,
		poller:   poller,
		failover: failoverH,
	}
}

// SetFailoverHandler sets the band failover handler reference.
func (h *CellularHandler) SetFailoverHandler(fo *BandFailoverHandler) {
	h.failover = fo
}

// Status returns current telemetry from poller.
func (h *CellularHandler) Status(w http.ResponseWriter, r *http.Request) {
	status := h.poller.GetStatus()
	JSON(w, http.StatusOK, status)
}

type SendATRequest struct {
	Command string `json:"command"`
}

// SendCommand executes a direct AT command via engine queue.
func (h *CellularHandler) SendCommand(w http.ResponseWriter, r *http.Request) {
	var req SendATRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		Error(w, http.StatusBadRequest, "Invalid or missing 'command' field")
		return
	}

	res, err := h.engine.Exec(req.Command)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":  false,
			"response": res.Raw,
			"error":    err.Error(),
			"command":  req.Command,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": res.Raw,
		"command":  req.Command,
	})
}

// CurrentBands holds the parsed configured bands per RAT as colon-delimited strings.
type CurrentBands struct {
	LTEBands     string `json:"lte_bands"`
	NSANR5GBands string `json:"nsa_nr5g_bands"`
	SANR5GBands  string `json:"sa_nr5g_bands"`
}

// SupportedBands holds the supported bands per RAT as arrays of string band numbers.
type SupportedBands struct {
	LTEBands     []string `json:"lte_bands"`
	NSANR5GBands []string `json:"nsa_nr5g_bands"`
	SANR5GBands  []string `json:"sa_nr5g_bands"`
}

// BandLockingResponse represents the response envelope for GetBands.
type BandLockingResponse struct {
	Success   bool           `json:"success"`
	Current   CurrentBands   `json:"current"`
	Supported SupportedBands `json:"supported"`
	Failover  FailoverState  `json:"failover"`
	Raw       string         `json:"raw,omitempty"`
	Data      interface{}    `json:"data,omitempty"`
}

var defaultSupportedLTEBands = []string{
	"1", "2", "3", "4", "5", "7", "8", "12", "13", "14", "17", "18", "19", "20",
	"25", "26", "28", "29", "30", "32", "34", "38", "39", "40", "41", "42", "43",
	"46", "48", "66", "71",
}

var defaultSupportedNRBands = []string{
	"1", "2", "3", "5", "7", "8", "12", "13", "14", "18", "20", "25", "26", "28",
	"29", "30", "38", "40", "41", "48", "66", "70", "71", "75", "76", "77", "78", "79",
}

func cleanBandStr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ':' || r == ',' || r == ' ' || r == '	' || r == ';'
	})
	var cleanParts []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "0" {
			cleanParts = append(cleanParts, p)
		}
	}
	return strings.Join(cleanParts, ":")
}

func splitBandString(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return []string{}
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ':' || r == ',' || r == ' ' || r == '	' || r == ';'
	})
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "0" {
			res = append(res, p)
		}
	}
	if len(res) == 0 {
		return []string{}
	}
	return res
}

// FailoverState holds the failover mechanism flags.
type FailoverState struct {
	Enabled        bool `json:"enabled"`
	Activated      bool `json:"activated"`
	WatcherRunning bool `json:"watcher_running"`
}

// GetBands returns active band locking configuration and failover status.
func (h *CellularHandler) GetBands(w http.ResponseWriter, r *http.Request) {
	cmd := `AT+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`
	res, err := h.engine.Exec(cmd)

	var bands CurrentBands
	rawOutput := ""
	if res != nil {
		rawOutput = res.Raw
	}

	needsFallback := err != nil ||
		!strings.Contains(rawOutput, "+QNWPREFCFG:") ||
		strings.Contains(rawOutput, "ERROR")

	if !needsFallback {
		bands = parseQNWPREFCFGBands(rawOutput)
		// If parsed bands are all empty, trigger fallback
		if bands.LTEBands == "" && bands.NSANR5GBands == "" && bands.SANR5GBands == "" {
			needsFallback = true
		}
	}

	if needsFallback {
		// Fallback for older firmware: AT+QCFG="band"
		resLegacy, errLegacy := h.engine.Exec(`AT+QCFG="band"`)
		if errLegacy == nil && resLegacy != nil && strings.Contains(resLegacy.Raw, "+QCFG:") {
			rawOutput = resLegacy.Raw
			bands = parseLegacyQCFGBands(resLegacy.Raw)
		} else if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to read band configuration")
			return
		}
	}

	supported := SupportedBands{
		LTEBands:     defaultSupportedLTEBands,
		NSANR5GBands: defaultSupportedNRBands,
		SANR5GBands:  defaultSupportedNRBands,
	}
	if h.poller != nil {
		st := h.poller.GetStatus()
		if s := splitBandString(st.Device.SupportedLTEBands); len(s) > 0 {
			supported.LTEBands = s
		}
		if s := splitBandString(st.Device.SupportedNSABands); len(s) > 0 {
			supported.NSANR5GBands = s
		}
		if s := splitBandString(st.Device.SupportedSABands); len(s) > 0 {
			supported.SANR5GBands = s
		}
	}

	failover := FailoverState{
		Enabled:        false,
		Activated:      false,
		WatcherRunning: false,
	}
	if h.failover != nil {
		failover.Enabled, failover.Activated, failover.WatcherRunning = h.failover.GetState()
	}

	resp := BandLockingResponse{
		Success:   true,
		Current:   bands,
		Supported: supported,
		Failover:  failover,
		Raw:       rawOutput,
		Data: map[string]interface{}{
			"current":   bands,
			"supported": supported,
			"failover":  failover,
			"raw":       rawOutput,
		},
	}

	JSON(w, http.StatusOK, resp)
}

func parseQNWPREFCFGBands(raw string) CurrentBands {
	var bands CurrentBands
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+QNWPREFCFG:") {
			payload := strings.TrimSpace(strings.TrimPrefix(l, "+QNWPREFCFG:"))
			parts := strings.SplitN(payload, ",", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.Trim(strings.TrimSpace(parts[0]), "\""))
				val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				switch key {
				case "lte_band":
					bands.LTEBands = cleanBandStr(val)
				case "nsa_nr5g_band":
					bands.NSANR5GBands = cleanBandStr(val)
				case "nr5g_band", "sa_nr5g_band":
					bands.SANR5GBands = cleanBandStr(val)
				}
			}
		}
	}
	return bands
}

func parseLegacyQCFGBands(raw string) CurrentBands {
	var bands CurrentBands
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+QCFG: \"band\"") || strings.HasPrefix(l, "+QCFG: \"band\",") || strings.HasPrefix(l, "+QCFG: band") {
			parts := strings.Split(l, ",")
			if len(parts) > 2 {
				lteMaskStr := strings.Trim(strings.TrimSpace(parts[2]), "\"")
				if strings.Contains(lteMaskStr, ":") {
					bands.LTEBands = cleanBandStr(lteMaskStr)
				} else {
					bands.LTEBands = cleanBandStr(parseHexMaskToBands(lteMaskStr))
				}
			}
			if len(parts) > 3 {
				nrMaskStr := strings.Trim(strings.TrimSpace(parts[3]), "\"")
				if strings.Contains(nrMaskStr, ":") {
					bands.SANR5GBands = cleanBandStr(nrMaskStr)
					bands.NSANR5GBands = cleanBandStr(nrMaskStr)
				} else {
					nrParsed := cleanBandStr(parseHexMaskToBands(nrMaskStr))
					bands.SANR5GBands = nrParsed
					bands.NSANR5GBands = nrParsed
				}
			}
		}
	}
	return bands
}

func parseHexMaskToBands(hexStr string) string {
	hexStr = strings.TrimPrefix(strings.TrimSpace(hexStr), "0x")
	hexStr = strings.TrimPrefix(hexStr, "0X")
	if hexStr == "" || hexStr == "0" {
		return ""
	}
	val, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return ""
	}
	var bands []string
	for bit := 0; bit < 64; bit++ {
		if (val & (1 << bit)) != 0 {
			bands = append(bands, strconv.Itoa(bit+1))
		}
	}
	return strings.Join(bands, ":")
}

// FlexibleStringSlice parses bands from string, string array, int array, or mixed types.
type FlexibleStringSlice []string

// FlexibleBands is an alias for FlexibleStringSlice for backward compatibility.
type FlexibleBands = FlexibleStringSlice

// UnmarshalJSON unmarshals array of strings, array of ints, or colon/comma/space-separated strings.
func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = nil
		return nil
	}
	var anySlice []interface{}
	if err := json.Unmarshal(data, &anySlice); err == nil {
		res := make([]string, 0, len(anySlice))
		for _, v := range anySlice {
			switch val := v.(type) {
			case string:
				s := strings.TrimSpace(val)
				if s != "" {
					res = append(res, s)
				}
			case float64:
				res = append(res, strconv.Itoa(int(val)))
			case int:
				res = append(res, strconv.Itoa(val))
			default:
				s := strings.TrimSpace(fmt.Sprintf("%v", val))
				if s != "" {
					res = append(res, s)
				}
			}
		}
		*f = res
		return nil
	}

	var singleStr string
	if err := json.Unmarshal(data, &singleStr); err == nil {
		singleStr = strings.TrimSpace(singleStr)
		if singleStr == "" {
			*f = []string{}
			return nil
		}
		parts := strings.FieldsFunc(singleStr, func(r rune) bool {
			return r == ':' || r == ',' || r == ' ' || r == '\t' || r == ';'
		})
		res := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				res = append(res, p)
			}
		}
		*f = res
		return nil
	}

	var singleNum float64
	if err := json.Unmarshal(data, &singleNum); err == nil {
		*f = []string{strconv.Itoa(int(singleNum))}
		return nil
	}

	return nil
}

// SetBandsRequest represents incoming band lock payloads.
type SetBandsRequest struct {
	BandType        string              `json:"band_type,omitempty"`
	Bands           FlexibleStringSlice `json:"bands,omitempty"`
	LTEBands        FlexibleStringSlice `json:"lte_bands,omitempty"`
	NRBands         FlexibleStringSlice `json:"nr_bands,omitempty"`
	NSANR5GBands    FlexibleStringSlice `json:"nsa_nr5g_bands,omitempty"`
	SANR5GBands     FlexibleStringSlice `json:"sa_nr5g_bands,omitempty"`
	Failover        *bool               `json:"failover,omitempty"`
	FailoverEnabled *bool               `json:"failover_enabled,omitempty"`
}

// LockBands applies band mask settings.
func (h *CellularHandler) LockBands(w http.ResponseWriter, r *http.Request) {
	var req SetBandsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid band configuration payload")
		return
	}

	if req.Failover != nil && h.failover != nil {
		h.failover.SetEnabled(*req.Failover)
	}
	if req.FailoverEnabled != nil && h.failover != nil {
		h.failover.SetEnabled(*req.FailoverEnabled)
	}

	// Normalize category if band_type is provided
	if req.BandType != "" && req.Bands != nil {
		switch strings.ToLower(req.BandType) {
		case "lte":
			req.LTEBands = req.Bands
		case "nsa_nr5g":
			req.NSANR5GBands = req.Bands
		case "sa_nr5g", "nr5g":
			req.SANR5GBands = req.Bands
		case "nr":
			if req.SANR5GBands == nil {
				req.SANR5GBands = req.Bands
			}
			if req.NRBands == nil {
				req.NRBands = req.Bands
			}
		}
	}

	var cmds []string
	if len(req.LTEBands) > 0 {
		lteMask := strings.Join(req.LTEBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="lte_band",%s`, lteMask))
	}
	if len(req.NSANR5GBands) > 0 {
		nsaMask := strings.Join(req.NSANR5GBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="nsa_nr5g_band",%s`, nsaMask))
	}
	if len(req.SANR5GBands) > 0 {
		saMask := strings.Join(req.SANR5GBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="nr5g_band",%s`, saMask))
	} else if len(req.NRBands) > 0 {
		nrMask := strings.Join(req.NRBands, ":")
		cmds = append(cmds, fmt.Sprintf(`AT+QNWPREFCFG="nr5g_band",%s`, nrMask))
	}

	for _, cmd := range cmds {
		if _, err := h.engine.Exec(cmd); err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed executing %s: %v", cmd, err))
			return
		}
	}

	failoverArmed := false
	if h.failover != nil && h.failover.IsEnabled() {
		failoverArmed = true
		h.failover.SetWatcherRunning(true)
		h.failover.SetActivated(false)
	}

	bandsStr := ""
	if req.Bands != nil {
		bandsStr = strings.Join(req.Bands, ":")
	} else if len(req.LTEBands) > 0 {
		bandsStr = strings.Join(req.LTEBands, ":")
	} else if len(req.SANR5GBands) > 0 {
		bandsStr = strings.Join(req.SANR5GBands, ":")
	} else if len(req.NSANR5GBands) > 0 {
		bandsStr = strings.Join(req.NSANR5GBands, ":")
	} else if len(req.NRBands) > 0 {
		bandsStr = strings.Join(req.NRBands, ":")
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"message":        "Bands updated successfully",
		"band_type":      req.BandType,
		"bands":          bandsStr,
		"failover_armed": failoverArmed,
		"data": map[string]interface{}{
			"message":        "Bands updated successfully",
			"band_type":      req.BandType,
			"bands":          bandsStr,
			"failover_armed": failoverArmed,
		},
	})
}

// LockTower handles PCI/EARFCN locking via AT+QNWLOCK.
func (h *CellularHandler) LockTower(w http.ResponseWriter, r *http.Request) {
	type TowerLockReq struct {
		Mode   string `json:"mode"`
		PCID   int    `json:"pcid"`
		PCI    int    `json:"pci,omitempty"`
		EARFCN int    `json:"earfcn"`
		ARFCN  int    `json:"arfcn,omitempty"`
		SCS    int    `json:"scs,omitempty"`
		Band   int    `json:"band,omitempty"`
	}

	var req TowerLockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid tower lock payload")
		return
	}

	pcid := req.PCI
	if req.PCID != 0 {
		pcid = req.PCID
	}

	earfcn := req.ARFCN
	if req.EARFCN != 0 {
		earfcn = req.EARFCN
	}

	var atCmd string
	if req.Mode == "5g" {
		if req.Band <= 0 {
			Error(w, http.StatusBadRequest, "band is required for 5G tower lock")
			return
		}
		scs := req.SCS
		if scs == 0 {
			scs = 30 // Default 30kHz for Sub-6
		}
		atCmd = fmt.Sprintf(`AT+QNWLOCK="common/5g",%d,%d,%d,%d`, pcid, earfcn, scs, req.Band)
	} else {
		atCmd = fmt.Sprintf(`AT+QNWLOCK="common/4g",1,%d,%d`, earfcn, pcid)
	}

	if _, err := h.engine.Exec(atCmd); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Tower lock failed: %v", err))
		return
	}

	Success(w, map[string]string{"message": "Tower locked successfully"})
}

// UnlockTower resets cell locks.
func (h *CellularHandler) UnlockTower(w http.ResponseWriter, r *http.Request) {
	cmds := []string{
		`AT+QNWLOCK="common/4g",0`,
		`AT+QNWLOCK="common/5g",0`,
		`AT+QNWLOCK="save_ctrl",0`,
	}

	for _, c := range cmds {
		_, _ = h.engine.Exec(c)
	}

	Success(w, map[string]string{"message": "Tower locks cleared"})
}

// HandleTowerLockCGI handles legacy POST /cgi-bin/quecmanager/tower/lock.sh
func (h *CellularHandler) HandleTowerLockCGI(w http.ResponseWriter, r *http.Request) {
	type CellItem struct {
		EARFCN int `json:"earfcn"`
		PCI    int `json:"pci"`
	}
	type CGITowerLockReq struct {
		Type   string     `json:"type"`   // "lte" or "nr_sa"
		Action string     `json:"action"` // "lock" or "unlock"
		Cells  []CellItem `json:"cells,omitempty"`
		PCI    int        `json:"pci,omitempty"`
		ARFCN  int        `json:"arfcn,omitempty"`
		SCS    int        `json:"scs,omitempty"`
		Band   int        `json:"band,omitempty"`
	}

	var req CGITowerLockReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	var atCmd string
	switch req.Type {
	case "lte", "4g":
		if req.Action == "unlock" {
			atCmd = `AT+QNWLOCK="common/4g",0`
		} else if req.Action == "lock" {
			if len(req.Cells) == 0 {
				Error(w, http.StatusBadRequest, "No cells provided for LTE lock")
				return
			}
			parts := make([]string, 0, len(req.Cells)*2)
			for _, c := range req.Cells {
				parts = append(parts, strconv.Itoa(c.EARFCN), strconv.Itoa(c.PCI))
			}
			atCmd = fmt.Sprintf(`AT+QNWLOCK="common/4g",%d,%s`, len(req.Cells), strings.Join(parts, ","))
		} else {
			Error(w, http.StatusBadRequest, "Invalid action for LTE tower lock")
			return
		}
	case "nr_sa", "5g", "nr5g":
		if req.Action == "unlock" {
			atCmd = `AT+QNWLOCK="common/5g",0`
		} else if req.Action == "lock" {
			if req.Band <= 0 {
				Error(w, http.StatusBadRequest, "band is required for 5G tower lock")
				return
			}
			scs := req.SCS
			if scs == 0 {
				scs = 30
			}
			atCmd = fmt.Sprintf(`AT+QNWLOCK="common/5g",%d,%d,%d,%d`, req.PCI, req.ARFCN, scs, req.Band)
		} else {
			Error(w, http.StatusBadRequest, "Invalid action for NR-SA tower lock")
			return
		}
	default:
		Error(w, http.StatusBadRequest, "Invalid lock type: must be 'lte' or 'nr_sa'")
		return
	}

	if _, err := h.engine.Exec(atCmd); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Tower lock failed: %v", err))
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"type":    req.Type,
		"action":  req.Action,
		"message": "Tower lock updated successfully",
	})
}
