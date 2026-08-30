package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"qmanager/internal/atengine"
)

// FplmnEntry represents a forbidden PLMN (MCC + MNC).
type FplmnEntry struct {
	Index int    `json:"index"`
	MCC   string `json:"mcc"`
	MNC   string `json:"mnc"`
	PLMN  string `json:"plmn"`
}

// CellularFplmnHandler handles FPLMN reading and clearing via SIM (AT+CRSM).
type CellularFplmnHandler struct {
	engine *atengine.Engine
}

// NewCellularFplmnHandler creates a CellularFplmnHandler.
func NewCellularFplmnHandler(engine *atengine.Engine) *CellularFplmnHandler {
	return &CellularFplmnHandler{engine: engine}
}

// GetFPLMN handles GET /api/v1/cellular/fplmn and GET /cgi-bin/quecmanager/cellular/fplmn.sh
func (h *CellularFplmnHandler) GetFPLMN(w http.ResponseWriter, r *http.Request) {
	// Query FPLMN file length: AT+CRSM=192,28539,0,0,15
	res, err := h.engine.Exec("AT+CRSM=192,28539,0,0,15")
	if err != nil || !strings.Contains(res.Raw, "+CRSM:") {
		Error(w, http.StatusInternalServerError, "Failed to query SIM FPLMN file information")
		return
	}

	length := parseCRSMFileLength(res.Raw)
	if length < 12 {
		length = 12 // Minimum 4 PLMNs
	}

	// Read FPLMN data: AT+CRSM=176,28539,0,0,<length>
	readCmd := fmt.Sprintf("AT+CRSM=176,28539,0,0,%d", length)
	res, err = h.engine.Exec(readCmd)
	if err != nil || !strings.Contains(res.Raw, "+CRSM:") {
		Error(w, http.StatusInternalServerError, "Failed to read FPLMN data from SIM")
		return
	}

	hexData := parseCRSMHexData(res.Raw)
	entries := ParseFplmnHex(hexData)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"fplmns":   entries,
		"count":    len(entries),
		"raw_data": hexData,
	})
}

// ClearFPLMN handles POST /api/v1/cellular/fplmn/clear and POST /cgi-bin/quecmanager/cellular/fplmn.sh
func (h *CellularFplmnHandler) ClearFPLMN(w http.ResponseWriter, r *http.Request) {
	// Query length first
	res, err := h.engine.Exec("AT+CRSM=192,28539,0,0,15")
	length := 12
	if err == nil && strings.Contains(res.Raw, "+CRSM:") {
		l := parseCRSMFileLength(res.Raw)
		if l >= 12 {
			length = l
		}
	}

	// Create all-FF hex string of required length
	ffData := strings.Repeat("F", length*2)

	// Write back via AT+CRSM=214,28539,0,0,<length>,"<ffData>"
	writeCmd := fmt.Sprintf(`AT+CRSM=214,28539,0,0,%d,"%s"`, length, ffData)
	res, err = h.engine.Exec(writeCmd)
	if err != nil || !strings.Contains(res.Raw, "+CRSM:") {
		Error(w, http.StatusInternalServerError, "Failed to clear FPLMN list on SIM")
		return
	}

	// Check status word (SW1=144 / 0x90, SW2=0 / 0x00 is success)
	sw1, sw2 := parseCRSMStatus(res.Raw)
	if sw1 != 144 && sw1 != 145 { // 144 = 0x90, 145 = 0x91
		Error(w, http.StatusInternalServerError, fmt.Sprintf("CRSM write error: SW1=%d SW2=%d", sw1, sw2))
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "FPLMN list cleared successfully",
	})
}

// ParseFplmnHex parses EF_FPLMN raw hex data into FPLMN entries.
// EF_FPLMN format: each PLMN is 3 bytes (6 hex chars).
// Byte 1: MCC digit 2 (high nibble) + MCC digit 1 (low nibble)
// Byte 2: MNC digit 3 (high nibble, 'F' if 2-digit MNC) + MCC digit 3 (low nibble)
// Byte 3: MNC digit 2 (high nibble) + MNC digit 1 (low nibble)
func ParseFplmnHex(hex string) []FplmnEntry {
	hex = strings.ToUpper(strings.TrimSpace(hex))
	var entries []FplmnEntry
	idx := 1

	for i := 0; i+6 <= len(hex); i += 6 {
		chunk := hex[i : i+6]
		if chunk == "FFFFFF" {
			continue // Empty slot
		}

		b1 := chunk[0:2]
		b2 := chunk[2:4]
		b3 := chunk[4:6]

		// b1 = [mcc2][mcc1]
		mcc1 := string(b1[1])
		mcc2 := string(b1[0])

		// b2 = [mnc3][mcc3]
		mcc3 := string(b2[1])
		mnc3 := string(b2[0])

		// b3 = [mnc2][mnc1]
		mnc1 := string(b3[1])
		mnc2 := string(b3[0])

		mcc := mcc1 + mcc2 + mcc3
		mnc := mnc1 + mnc2
		if mnc3 != "F" {
			mnc += mnc3
		}

		plmn := mcc + mnc
		entries = append(entries, FplmnEntry{
			Index: idx,
			MCC:   mcc,
			MNC:   mnc,
			PLMN:  plmn,
		})
		idx++
	}

	return entries
}

func parseCRSMFileLength(raw string) int {
	// +CRSM: <sw1>,<sw2>[,"<data>"]
	// When querying file info (192), data format has length at byte 2-3 (hex chars 4-7)
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CRSM:") {
			re := regexp.MustCompile(`\+CRSM:\s*\d+,\d+,"?([0-9A-Fa-f]+)"?`)
			m := re.FindStringSubmatch(l)
			if len(m) > 1 {
				hexData := m[1]
				if len(hexData) >= 8 {
					// file length is typically bytes 2-3 (hex pos 4..7)
					lenHex := hexData[4:8]
					if val, err := strconv.ParseInt(lenHex, 16, 32); err == nil && val > 0 {
						return int(val)
					}
				}
			}
		}
	}
	return 12
}

func parseCRSMHexData(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CRSM:") {
			re := regexp.MustCompile(`\+CRSM:\s*\d+,\d+,"?([0-9A-Fa-f]+)"?`)
			m := re.FindStringSubmatch(l)
			if len(m) > 1 {
				return m[1]
			}
		}
	}
	return ""
}

func parseCRSMStatus(raw string) (int, int) {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CRSM:") {
			parts := strings.Split(strings.TrimPrefix(l, "+CRSM:"), ",")
			if len(parts) >= 2 {
				sw1, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
				sw2, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				return sw1, sw2
			}
		}
	}
	return 0, 0
}
