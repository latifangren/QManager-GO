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

// ParseQENGServingCell parses `+QENG: "servingcell",...` lines.
func ParseQENGServingCell(raw string) *CellInfo {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QENG: \"servingcell\",") {
			continue
		}

		parts := parseCSVLine(strings.TrimPrefix(line, "+QENG: \"servingcell\","))
		if len(parts) < 3 {
			continue
		}

		info := &CellInfo{
			State: parts[0],
			Mode:  parts[1],
		}

		if info.State == "SEARCH" || info.State == "LIMSRV" {
			return info
		}

		switch info.Mode {
		case "LTE", "eMMT", "NB-IoT":
			// LTE: "CONNECT"/"NOCONN", "LTE", is_tdd, mcc, mnc, cellid, pcid, earfcn, freq_band_ind, ul_bandwidth, dl_bandwidth, tac, rsrp, rsrq, rssi, sinr, cqi, tx_power, srxlev
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
			// NR5G-SA: "CONNECT"/"NOCONN", "NR5G-SA", is_tdd, mcc, mnc, cellid, pcid, tac, arfcn, band, nr_dl_bandwidth, rsrp, rsrq, sinr, tx_power, srxlev
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
		}

		return info
	}

	return nil
}

// ParseQCAINFO parses `+QCAINFO: ...` lines for carrier aggregation.
func ParseQCAINFO(raw string) []CarrierComponent {
	var list []CarrierComponent
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
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
		rb, _ := strconv.Atoi(parts[2])
		bandStr := parts[3]
		pcid, _ := strconv.Atoi(parts[5])

		bwMHz := 0
		switch rb {
		case 6:
			bwMHz = 1
		case 15:
			bwMHz = 3
		case 25:
			bwMHz = 5
		case 50:
			bwMHz = 10
		case 75:
			bwMHz = 15
		case 100:
			bwMHz = 20
		default:
			if rb > 0 {
				bwMHz = rb / 5
			}
		}

		tech := "LTE"
		if strings.Contains(strings.ToUpper(bandStr), "NR") {
			tech = "NR"
		}

		cleanBand := bandStr
		if strings.Contains(bandStr, "BAND ") {
			idx := strings.Index(bandStr, "BAND ")
			num := bandStr[idx+5:]
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

// ParseCSQ parses `+CSQ: <rssi>,<ber>` response.
func ParseCSQ(raw string) *SignalQuality {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CSQ:") {
			continue
		}

		parts := parseCSVLine(strings.TrimPrefix(line, "+CSQ:"))
		if len(parts) < 2 {
			continue
		}

		rssi, _ := strconv.Atoi(parts[0])
		ber, _ := strconv.Atoi(parts[1])

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

	return nil
}

func parseCSVLine(line string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false

	for _, ch := range line {
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if inQuotes {
				cur.WriteRune(ch)
			} else {
				parts = append(parts, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	parts = append(parts, strings.TrimSpace(cur.String()))

	return parts
}
