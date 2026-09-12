package atengine

import (
	"strconv"
	"strings"
)

// CellInfo holds parsed cell radio telemetry from +QENG: "servingcell".
type CellInfo struct {
	State        string `json:"state"`        // "SEARCH", "LIMSRV", "NOCONN", "CONNECT"
	Mode         string `json:"mode"`         // "LTE", "NR5G-SA", "NR5G-NSA", "WCDMA"
	Duplex       string `json:"duplex"`       // "FDD", "TDD"
	MCC          string `json:"mcc"`          // Mobile Country Code
	MNC          string `json:"mnc"`          // Mobile Network Code
	CellID       string `json:"cell_id"`      // Hex or Dec Cell ID
	PCID         int    `json:"pcid"`         // Physical Cell ID
	EARFCN       int    `json:"earfcn"`       // DL EARFCN / ARFCN
	Band         string `json:"band"`         // Frequency Band (e.g. "B1", "B3", "n78")
	ULBandwidth  string `json:"ul_bandwidth"` // UL Bandwidth (e.g. "20M")
	DLBandwidth  string `json:"dl_bandwidth"` // DL Bandwidth (e.g. "20M")
	Bandwidth    string `json:"bandwidth"`    // Bandwidth
	RSRP         int    `json:"rsrp"`         // Reference Signal Received Power (dBm)
	RSRQ         int    `json:"rsrq"`         // Reference Signal Received Quality (dB)
	RSSI         int    `json:"rssi"`         // Received Signal Strength Indicator (dBm)
	SINR         int    `json:"sinr"`         // Signal to Interference plus Noise Ratio (dB)
	CQI          int    `json:"cqi"`          // Channel Quality Indicator
	TAC          string `json:"tac"`          // Tracking Area Code (Hex)

	// 5G NSA Secondary Carrier Info (when in EN-DC / NR5G-NSA)
	HasNR5GNSA bool   `json:"has_nr5g_nsa"`
	NR5GState  string `json:"nr5g_state,omitempty"`
	NR5GBand   string `json:"nr5g_band,omitempty"`
	NR5GARFCN  int    `json:"nr5g_arfcn,omitempty"`
	NR5GPCI    int    `json:"nr5g_pci,omitempty"`
	NR5GRSRP   int    `json:"nr5g_rsrp,omitempty"`
	NR5GRSRQ   int    `json:"nr5g_rsrq,omitempty"`
	NR5GSINR   int    `json:"nr5g_sinr,omitempty"`
	NR5GSCS    string `json:"nr5g_scs,omitempty"`
}

// CarrierComponent represents a component carrier in CA matching both frontend and backend contracts.
type CarrierComponent struct {
	Role         string `json:"role,omitempty"`          // "PCC", "SCC"
	Type         string `json:"type"`                    // "PCC", "SCC"
	Technology   string `json:"technology,omitempty"`    // "LTE", "NR"
	Band         string `json:"band"`                    // "B1", "B3", "n78"
	EARFCN       int    `json:"earfcn"`                  // Channel frequency
	Bandwidth    string `json:"bandwidth,omitempty"`     // "20M", "10M"
	BandwidthMHz int    `json:"bandwidth_mhz,omitempty"` // 20, 10
	PCID         int    `json:"pcid,omitempty"`          // Physical Cell ID
	PCI          int    `json:"pci,omitempty"`           // Physical Cell ID
	RSRP         int    `json:"rsrp"`                    // Signal Power
	RSRQ         int    `json:"rsrq"`                    // Signal Quality
	RSSI         int    `json:"rssi"`                    // RSSI
	SINR         int    `json:"sinr"`                    // SINR
}

// SignalQuality represents CSQ signal reading.
type SignalQuality struct {
	RSSI    int `json:"rssi_raw"`
	BER     int `json:"ber_raw"`
	RSRPDbm int `json:"rssi_dbm"`
}

// ParseQENGServingCell parses `+QENG: "servingcell",...` lines (both single-line and multi-line NSA).
func ParseQENGServingCell(raw string) *CellInfo {
	if strings.TrimSpace(raw) == "" || strings.Contains(raw, "ERROR") {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var scLine, lteLine, nrNsaLine string

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, `+QENG: "servingcell"`) || strings.HasPrefix(l, `+QENG: servingcell`) {
			scLine = l
		} else if strings.HasPrefix(l, `+QENG: "LTE"`) || strings.HasPrefix(l, `+QENG: LTE`) {
			lteLine = l
		} else if strings.HasPrefix(l, `+QENG: "NR5G-NSA"`) || strings.HasPrefix(l, `+QENG: NR5G-NSA`) {
			nrNsaLine = l
		}
	}

	// If no line matched with prefixes, try fallback finding "+QENG: \"servingcell\""
	if scLine == "" && lteLine == "" && nrNsaLine == "" {
		idx := strings.Index(raw, `+QENG: "servingcell"`)
		if idx == -1 {
			return nil
		}
		scLine = raw[idx:]
		if end := strings.IndexAny(scLine, "\r\n"); end != -1 {
			scLine = scLine[:end]
		}
	}

	info := &CellInfo{}

	// 1. Process servingcell line
	if scLine != "" {
		prefix := `+QENG: "servingcell",`
		var content string
		if strings.HasPrefix(scLine, prefix) {
			content = scLine[len(prefix):]
		} else {
			content = strings.TrimPrefix(scLine, `+QENG: "servingcell"`)
			content = strings.TrimPrefix(content, ",")
		}

		parts := parseCSVLine(content)
		if len(parts) > 0 && parts[0] != "" {
			info.State = parts[0]
		} else if lteLine == "" && nrNsaLine == "" {
			return nil
		}
		if len(parts) > 1 {
			info.Mode = parts[1]
		}

		if info.State == "SEARCH" || info.State == "LIMSRV" {
			return info
		}

		// Single-line format (e.g. +QENG: "servingcell","NOCONN","LTE","FDD",...)
		if len(parts) >= 3 {
			switch info.Mode {
			case "LTE", "eMMT", "NB-IoT":
				if len(parts) >= 16 {
					info.Duplex = parts[2]
					info.MCC = parts[3]
					info.MNC = parts[4]
					info.CellID = parts[5]
					info.PCID, _ = strconv.Atoi(parts[6])
					info.EARFCN, _ = strconv.Atoi(parts[7])
					info.Band = "B" + parts[8]
					info.ULBandwidth = parts[9]
					info.DLBandwidth = parts[10]
					info.Bandwidth = parts[10]
					info.TAC = parts[11]
					info.RSRP, _ = strconv.Atoi(parts[12])
					info.RSRQ, _ = strconv.Atoi(parts[13])
					info.RSSI, _ = strconv.Atoi(parts[14])
					info.SINR, _ = strconv.Atoi(parts[15])
					if len(parts) >= 17 {
						info.CQI, _ = strconv.Atoi(parts[16])
					}
				}
			case "NR5G-SA":
				if len(parts) >= 14 {
					info.Duplex = parts[2]
					info.MCC = parts[3]
					info.MNC = parts[4]
					info.CellID = parts[5]
					info.PCID, _ = strconv.Atoi(parts[6])
					info.TAC = parts[7]
					info.EARFCN, _ = strconv.Atoi(parts[8])
					info.Band = "n" + parts[9]
					info.DLBandwidth = parts[10]
					info.Bandwidth = parts[10]
					info.RSRP, _ = strconv.Atoi(parts[11])
					info.RSRQ, _ = strconv.Atoi(parts[12])
					info.SINR, _ = strconv.Atoi(parts[13])
				}
			case "NR5G-NSA":
				if len(parts) >= 10 {
					info.MCC = parts[2]
					info.MNC = parts[3]
					info.PCID, _ = strconv.Atoi(parts[4])
					info.RSRP, _ = strconv.Atoi(parts[5])
					info.SINR, _ = strconv.Atoi(parts[6])
					info.RSRQ, _ = strconv.Atoi(parts[7])
					info.EARFCN, _ = strconv.Atoi(parts[8])
					info.Band = "n" + parts[9]
					if len(parts) >= 11 {
						info.DLBandwidth = parts[10]
						info.Bandwidth = parts[10]
					}
				}
			case "WCDMA":
				if len(parts) >= 10 {
					info.MCC = parts[2]
					info.MNC = parts[3]
					info.TAC = parts[4]
					info.CellID = parts[5]
					info.EARFCN, _ = strconv.Atoi(parts[6])
					info.PCID, _ = strconv.Atoi(parts[7])
					info.RSRP, _ = strconv.Atoi(parts[9])
				}
			}
		}
	}

	// 2. Parse multi-line LTE line: +QENG: "LTE","FDD",510,09,B767015,418,9285,28,4,4,CD85,-104,-13,-74,10,8,200,-
	if lteLine != "" {
		content := strings.TrimPrefix(lteLine, `+QENG: `)
		parts := parseCSVLine(content)
		if len(parts) >= 15 && parts[0] == "LTE" {
			info.Mode = "LTE"
			info.Duplex = parts[1]
			info.MCC = parts[2]
			info.MNC = parts[3]
			info.CellID = parts[4]
			info.PCID, _ = strconv.Atoi(parts[5])
			info.EARFCN, _ = strconv.Atoi(parts[6])
			info.Band = "B" + parts[7]
			info.ULBandwidth = parts[8]
			info.DLBandwidth = parts[9]
			info.Bandwidth = parts[9]
			info.TAC = parts[10]
			info.RSRP, _ = strconv.Atoi(parts[11])
			info.RSRQ, _ = strconv.Atoi(parts[12])
			info.RSSI, _ = strconv.Atoi(parts[13])
			info.SINR, _ = strconv.Atoi(parts[14])
			if len(parts) >= 16 {
				info.CQI, _ = strconv.Atoi(parts[15])
			}
		}
	}

	// 3. Parse multi-line NR5G-NSA line: +QENG: "NR5G-NSA",510,09,357,-110,-4,-17,504990,41,7,1
	if nrNsaLine != "" {
		content := strings.TrimPrefix(nrNsaLine, `+QENG: `)
		parts := parseCSVLine(content)
		if len(parts) >= 9 && parts[0] == "NR5G-NSA" {
			info.HasNR5GNSA = true
			info.NR5GState = "connected"
			if info.State == "NOCONN" {
				info.NR5GState = "idle"
			}
			info.NR5GPCI, _ = strconv.Atoi(parts[3])
			info.NR5GRSRP, _ = strconv.Atoi(parts[4])
			info.NR5GSINR, _ = strconv.Atoi(parts[5])
			info.NR5GRSRQ, _ = strconv.Atoi(parts[6])
			info.NR5GARFCN, _ = strconv.Atoi(parts[7])
			info.NR5GBand = "n" + parts[8]
			if len(parts) >= 11 {
				switch parts[10] {
				case "0":
					info.NR5GSCS = "15kHz"
				case "1":
					info.NR5GSCS = "30kHz"
				case "2":
					info.NR5GSCS = "60kHz"
				case "3":
					info.NR5GSCS = "120kHz"
				case "4":
					info.NR5GSCS = "240kHz"
				default:
					info.NR5GSCS = parts[10]
				}
			}
			info.Mode = "NR5G-NSA"
		}
	}

	if info.State == "" {
		if lteLine != "" || nrNsaLine != "" {
			info.State = "NOCONN"
		} else if info.Mode == "" {
			return nil
		}
	}

	return info
}

// ParseQCAINFO parses `+QCAINFO: ...` lines for carrier aggregation.
func ParseQCAINFO(raw string) []CarrierComponent {
	var list []CarrierComponent
	remaining := raw

	for len(remaining) > 0 {
		var line string
		if idx := strings.IndexByte(remaining, '\n'); idx != -1 {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			line = remaining
			remaining = ""
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QCAINFO:") {
			continue
		}

		content := strings.TrimPrefix(line, "+QCAINFO:")
		parts := parseCSVLine(content)
		if len(parts) < 6 {
			continue
		}

		// Example: "PCC",1675,100,"LTE BAND 3",1,218,-85,-9,-62,18
		role := parts[0]
		earfcn, _ := strconv.Atoi(parts[1])
		rawBW := parts[2]
		bandStr := parts[3]
		pcid, _ := strconv.Atoi(parts[5])

		tech := "LTE"
		upperBand := strings.ToUpper(bandStr)
		if strings.Contains(upperBand, "NR") || strings.HasPrefix(upperBand, "N") {
			tech = "NR"
		}

		bwMHz := parseBandwidthMHz(rawBW, tech)

		cleanBand := bandStr
		if strings.Contains(upperBand, "BAND ") {
			idx := strings.Index(upperBand, "BAND ")
			num := strings.TrimSpace(bandStr[idx+5:])
			if tech == "NR" {
				cleanBand = "n" + num
			} else {
				cleanBand = "B" + num
			}
		} else if strings.Contains(upperBand, "BAND") {
			idx := strings.Index(upperBand, "BAND")
			num := strings.TrimSpace(bandStr[idx+4:])
			if tech == "NR" {
				cleanBand = "n" + num
			} else {
				cleanBand = "B" + num
			}
		}

		cc := CarrierComponent{
			Role:         role,
			Type:         role,
			Technology:   tech,
			EARFCN:       earfcn,
			Band:         cleanBand,
			Bandwidth:    strconv.Itoa(bwMHz) + "M",
			BandwidthMHz: bwMHz,
			PCID:         pcid,
			PCI:          pcid,
		}

		if len(parts) >= 10 {
			cc.RSRP, _ = strconv.Atoi(parts[6])
			cc.RSRQ, _ = strconv.Atoi(parts[7])
			cc.RSSI, _ = strconv.Atoi(parts[8])
			cc.SINR, _ = strconv.Atoi(parts[9])
		}

		list = append(list, cc)
	}

	return list
}

// ParseBandwidthMHz converts raw bandwidth string into MHz integer.
func ParseBandwidthMHz(rawBW string, tech string) int {
	rawBW = strings.Trim(strings.TrimSpace(rawBW), "\"")
	if rawBW == "" {
		return 0
	}
	upper := strings.ToUpper(rawBW)

	// String suffixes: "MHZ", "M", "KHZ", "K"
	if strings.HasSuffix(upper, "MHZ") {
		numStr := strings.TrimSpace(strings.TrimSuffix(upper, "MHZ"))
		if v, err := strconv.Atoi(numStr); err == nil {
			return v
		}
		if f, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int(f)
		}
	}
	if strings.HasSuffix(upper, "M") {
		numStr := strings.TrimSpace(strings.TrimSuffix(upper, "M"))
		if v, err := strconv.Atoi(numStr); err == nil {
			return v
		}
		if f, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int(f)
		}
	}
	if strings.HasSuffix(upper, "KHZ") {
		numStr := strings.TrimSpace(strings.TrimSuffix(upper, "KHZ"))
		if v, err := strconv.Atoi(numStr); err == nil {
			return v / 1000
		}
	}
	if strings.HasSuffix(upper, "K") {
		numStr := strings.TrimSpace(strings.TrimSuffix(upper, "K"))
		if v, err := strconv.Atoi(numStr); err == nil {
			return v / 1000
		}
	}

	val, err := strconv.Atoi(rawBW)
	if err != nil {
		return 0
	}
	if val < 0 {
		return 0
	}

	// Value in kHz (e.g. 100000 kHz = 100 MHz, 80000 kHz = 80 MHz, 20000 kHz = 20 MHz)
	if val >= 1000 {
		return val / 1000
	}

	if tech == "NR" {
		// Common NR Resource Blocks (SCS 30kHz / 15kHz)
		switch val {
		case 273:
			return 100
		case 217, 216:
			return 80
		case 162, 160:
			return 60
		case 133, 135:
			return 50
		case 106:
			return 40
		case 79:
			return 15
		case 51, 52:
			return 20
		case 24, 25:
			return 10
		}
		// Direct NR bandwidth in MHz (e.g. 100, 90, 80, 70, 60, 50, 40, 30, 25, 20, 15, 10, 5)
		if val == 100 || val == 90 || val == 80 || val == 70 || val == 60 || val == 50 || val == 40 || val == 30 || val == 25 || val == 20 || val == 15 || val == 10 || val == 5 {
			return val
		}
		return val
	}

	// LTE Bandwidth Codes (3GPP / Quectel: 0=1.4M, 1=3M, 2=5M, 3=10M, 4=15M, 5=20M)
	if tech == "LTE" || (tech != "NR" && val <= 5) {
		switch val {
		case 0:
			return 1 // 1.4 MHz (approx 1 MHz)
		case 1:
			return 3
		case 2:
			return 5
		case 3:
			return 10
		case 4:
			return 15
		case 5:
			return 20
		}
	}

	// LTE Resource Blocks (RB)
	switch val {
	case 6:
		return 1 // 1.4 MHz (approx 1 MHz)
	case 15:
		return 3
	case 25:
		return 5
	case 50:
		return 10
	case 75:
		return 15
	case 100:
		return 20
	default:
		if val <= 20 {
			return val
		}
		return val / 5
	}
}

func parseBandwidthMHz(rawBW string, tech string) int {
	return ParseBandwidthMHz(rawBW, tech)
}

// BatteryStatus holds parsed battery info from +CBC.
type BatteryStatus struct {
	BCS     int `json:"bcs"`     // Battery connection status (0: powered by battery, 1: battery connected, 2: no battery)
	BCL     int `json:"bcl"`     // Battery charge level (0-100%)
	Voltage int `json:"voltage"` // Battery voltage in mV (if reported)
}

// TemperatureInfo holds parsed module temperature sensors from +QTEMP.
type TemperatureInfo struct {
	Sensors map[string]int `json:"sensors"` // Sensor name to temperature in Celsius
	MaxTemp int            `json:"max_temp"`
}

var urcPrefixes = []string{
	"+QIURC:",
	"+CMTI:",
	"+CEREG:",
	"+CGREG:",
	"+CREG:",
	"+QIND:",
	"+QSIMSTAT:",
	"^MODE:",
	"^DSFLOWRPT:",
	"RDY",
	"RING",
	"NO CARRIER",
}

// FilterURC removes common asynchronous Unsolicited Result Codes from raw AT responses.
func FilterURC(raw string) string {
	hasURC := false
	for _, prefix := range urcPrefixes {
		if strings.Contains(raw, prefix) {
			hasURC = true
			break
		}
	}
	if !hasURC {
		return raw
	}

	lines := strings.Split(raw, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		isURC := false
		for _, prefix := range urcPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				isURC = true
				break
			}
		}
		if !isURC {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

// ParseCBC parses `+CBC: <bcs>,<bcl>[,<voltage>]` response.
func ParseCBC(raw string) *BatteryStatus {
	clean := FilterURC(raw)
	lines := strings.Split(clean, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CBC:") {
			continue
		}

		parts := parseCSVLine(strings.TrimPrefix(line, "+CBC:"))
		if len(parts) < 2 {
			continue
		}

		bcs, err1 := strconv.Atoi(parts[0])
		bcl, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}

		volt := 0
		if len(parts) >= 3 {
			volt, _ = strconv.Atoi(parts[2])
		}

		return &BatteryStatus{
			BCS:     bcs,
			BCL:     bcl,
			Voltage: volt,
		}
	}
	return nil
}

// ParseQTEMP parses `+QTEMP: ...` lines reporting thermals.
// Example:
// +QTEMP: "xo_therm_buf","35"
// +QTEMP: "mdm_case_therm","38"
// +QTEMP: "pa_therm0","34"
func ParseQTEMP(raw string) *TemperatureInfo {
	clean := FilterURC(raw)
	lines := strings.Split(clean, "\n")
	sensors := make(map[string]int)
	maxT := -999

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QTEMP:") {
			continue
		}

		parts := parseCSVLine(strings.TrimPrefix(line, "+QTEMP:"))
		if len(parts) < 2 {
			continue
		}

		name := strings.Trim(parts[0], `"`)
		val, err := strconv.Atoi(strings.Trim(parts[1], `"`))
		if err != nil {
			continue
		}

		sensors[name] = val
		if val > maxT {
			maxT = val
		}
	}

	if len(sensors) == 0 {
		return nil
	}

	return &TemperatureInfo{
		Sensors: sensors,
		MaxTemp: maxT,
	}
}

// ParseCSQ parses `+CSQ: <rssi>,<ber>` response.
func ParseCSQ(raw string) *SignalQuality {
	idx := strings.Index(raw, "+CSQ:")
	if idx == -1 {
		return nil
	}
	line := raw[idx+5:]
	if end := strings.IndexByte(line, '\r'); end != -1 {
		line = line[:end]
	} else if end := strings.IndexByte(line, '\n'); end != -1 {
		line = line[:end]
	}

	parts := parseCSVLine(line)
	if len(parts) < 2 {
		return nil
	}

	rssi, err1 := strconv.Atoi(parts[0])
	ber, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}

	dbm := 0
	if rssi == 0 {
		dbm = -113
	} else if rssi == 1 {
		dbm = -111
	} else if rssi >= 2 && rssi <= 30 {
		dbm = -109 + (rssi-2)*2
	} else if rssi == 31 {
		dbm = -51
	}

	return &SignalQuality{
		RSSI:    rssi,
		BER:     ber,
		RSRPDbm: dbm,
	}
}

func parseCSVLine(line string) []string {
	var parts []string
	start := 0
	inQuotes := false
	n := len(line)

	for i := 0; i < n; i++ {
		ch := line[i]
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				part := strings.TrimSpace(line[start:i])
				part = strings.Trim(part, `"`)
				parts = append(parts, part)
				start = i + 1
			}
		}
	}
	if start <= n {
		part := strings.TrimSpace(line[start:])
		part = strings.Trim(part, `"`)
		parts = append(parts, part)
	}

	return parts
}

// ParseCOPS extracts operator/carrier name from `+COPS: ...` response.
// Example: `+COPS: 0,0,"Smartfren Jagoan Sinyal  Smartfren",13` -> "Smartfren Jagoan Sinyal  Smartfren"
func ParseCOPS(raw string) string {
	idx := strings.Index(raw, `+COPS:`)
	if idx == -1 {
		return ""
	}
	line := raw[idx:]
	if end := strings.IndexAny(line, "\r\n"); end != -1 {
		line = line[:end]
	}
	firstQuote := strings.IndexByte(line, '"')
	if firstQuote == -1 {
		return ""
	}
	secondQuote := strings.IndexByte(line[firstQuote+1:], '"')
	if secondQuote == -1 {
		return ""
	}
	return strings.TrimSpace(line[firstQuote+1 : firstQuote+1+secondQuote])
}

// ParseCPIN extracts SIM PIN status from `+CPIN: ...` response.
// Example: `+CPIN: READY` -> "READY"
func ParseCPIN(raw string) string {
	idx := strings.Index(raw, `+CPIN:`)
	if idx == -1 {
		return ""
	}
	line := raw[idx:]
	if end := strings.IndexAny(line, "\r\n"); end != -1 {
		line = line[:end]
	}
	val := strings.TrimPrefix(line, `+CPIN:`)
	return strings.TrimSpace(val)
}

// ParseCGSN extracts 15-digit IMEI from `AT+CGSN` or `AT+GSN` response.
func ParseCGSN(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) >= 14 && len(l) <= 17 && !strings.Contains(l, "OK") && !strings.Contains(l, "AT") {
			return l
		}
	}
	return ""
}

// ParseCIMI extracts IMSI from `AT+CIMI` response.
func ParseCIMI(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) >= 14 && len(l) <= 16 && !strings.Contains(l, "OK") && !strings.Contains(l, "AT") {
			return l
		}
	}
	return ""
}

// ParseQCCID extracts ICCID from `+QCCID: ...` response.
func ParseQCCID(raw string) string {
	idx := strings.Index(raw, `+QCCID:`)
	if idx != -1 {
		line := raw[idx:]
		if end := strings.IndexAny(line, "\r\n"); end != -1 {
			line = line[:end]
		}
		val := strings.TrimPrefix(line, `+QCCID:`)
		return strings.TrimSpace(val)
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) >= 18 && len(l) <= 22 && !strings.Contains(l, "OK") && !strings.Contains(l, "AT") {
			return l
		}
	}
	return ""
}

// ParseAntennaSignals parses `+QRSRP: ...`, `+QRSRQ: ...`, `+QSINR: ...` lines into 4-element integer pointers.
// Sentinel -32768 is mapped to nil.
// Example: `+QRSRP: -104,-112,-32768,-32768,LTE` -> lte = [-104, -112, nil, nil]
func ParseAntennaSignals(raw string, prefix string) (lte []*int, nr []*int) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	targetPrefix := "+" + prefix + ":"

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, targetPrefix) {
			continue
		}
		content := strings.TrimSpace(strings.TrimPrefix(l, targetPrefix))
		parts := strings.Split(content, ",")
		if len(parts) < 4 {
			continue
		}

		vals := make([]*int, 4)
		for i := 0; i < 4; i++ {
			if i < len(parts) {
				s := strings.TrimSpace(parts[i])
				if n, err := strconv.Atoi(s); err == nil {
					if n != -32768 {
						val := n
						vals[i] = &val
					}
				}
			}
		}

		rat := "LTE"
		if len(parts) >= 5 {
			rat = strings.ToUpper(strings.TrimSpace(parts[4]))
		}

		if strings.Contains(rat, "NR") {
			nr = vals
		} else {
			lte = vals
		}
	}
	return lte, nr
}

// ParseCFUN parses `+CFUN: <n>` response. Returns 1 by default.
func ParseCFUN(raw string) int {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+CFUN:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(l, "+CFUN:"))
			if v, err := strconv.Atoi(valStr); err == nil {
				return v
			}
		}
	}
	return 1
}

// ParseTimeAdvance parses `+QNWCFG: "lte_time_advance",1,<ta>` or `+QNWCFG: "nr5g_time_advance",1,<nta>,...`
func ParseTimeAdvance(raw string, isNR bool) *int {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.Contains(l, "time_advance") {
			continue
		}
		parts := parseCSVLine(l)
		// Example: "+QNWCFG: "lte_time_advance", 1, 1344" -> len 3
		if len(parts) >= 3 {
			valStr := strings.TrimSpace(parts[2])
			if val, err := strconv.Atoi(valStr); err == nil && val > 0 {
				if !isNR && val > 1282 {
					// 3GPP 36.213: If modem returns NTA (multiple of 16), convert to TA index (0..1282)
					val = val / 16
				}
				return &val
			}
		}
	}
	return nil
}

// SupportedBandsInfo contains colon-delimited supported band strings.
type SupportedBandsInfo struct {
	LTEBands     string
	NSANR5GBands string
	SANR5GBands  string
}

// ParsePolicyBand parses AT+QNWPREFCFG="policy_band" output.
func ParsePolicyBand(raw string) SupportedBandsInfo {
	var info SupportedBandsInfo
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "+QNWPREFCFG:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(l, "+QNWPREFCFG:"))
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.Trim(strings.TrimSpace(parts[0]), "\""))
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch key {
		case "lte_band":
			info.LTEBands = val
		case "nsa_nr5g_band":
			info.NSANR5GBands = val
		case "nr5g_band", "sa_nr5g_band":
			info.SANR5GBands = val
		}
	}
	return info
}

