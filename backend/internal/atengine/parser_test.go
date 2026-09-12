package atengine

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQENGServingCell_LTE(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`
	info := ParseQENGServingCell(raw)
	if info == nil {
		t.Fatalf("expected non-nil CellInfo")
	}
	if info.Mode != "LTE" {
		t.Errorf("expected Mode=LTE, got %s", info.Mode)
	}
	if info.Band != "B3" {
		t.Errorf("expected Band=B3, got %s", info.Band)
	}
	if info.PCID != 218 {
		t.Errorf("expected PCID=218, got %d", info.PCID)
	}
	if info.RSRP != -85 || info.RSRQ != -9 || info.SINR != 18 {
		t.Errorf("signal mismatch: %+v", info)
	}
}

func TestParseQENGServingCell_NR5G(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN","NR5G-SA","TDD",510,11,402100,320,1234,627392,78,100,-78,-10,25`
	info := ParseQENGServingCell(raw)
	if info == nil {
		t.Fatalf("expected non-nil CellInfo")
	}
	if info.Mode != "NR5G-SA" {
		t.Errorf("expected Mode=NR5G-SA, got %s", info.Mode)
	}
	if info.Band != "n78" {
		t.Errorf("expected Band=n78, got %s", info.Band)
	}
	if info.PCID != 320 {
		t.Errorf("expected PCID=320, got %d", info.PCID)
	}
	if info.RSRP != -78 || info.RSRQ != -10 || info.SINR != 25 {
		t.Errorf("signal mismatch: %+v", info)
	}
}

func TestParseQENGServingCell_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *CellInfo
	}{
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "search state",
			input:    `+QENG: "servingcell","SEARCH"`,
			expected: &CellInfo{State: "SEARCH", Mode: ""},
		},
		{
			name:     "limsrv state",
			input:    `+QENG: "servingcell","LIMSRV","LTE"`,
			expected: &CellInfo{State: "LIMSRV", Mode: "LTE"},
		},
		{
			name:     "malformed prefix",
			input:    `+QENG: "neighbourcell",...`,
			expected: nil,
		},
		{
			name:     "with URC noise interleaved",
			input:    "+QIURC: \"recv\",0,4\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-\nRING\n",
			expected: &CellInfo{State: "NOCONN", Mode: "LTE", Duplex: "FDD", MCC: "510", MNC: "11", CellID: "1A2B3C", PCID: 218, EARFCN: 1675, Band: "B3", ULBandwidth: "5", DLBandwidth: "5", Bandwidth: "5", TAC: "9A4F", RSRP: -85, RSRQ: -9, RSSI: -62, SINR: 18},
		},
		{
			name:     "NR5G-NSA mode",
			input:    `+QENG: "servingcell","NOCONN","NR5G-NSA",510,11,320,-78,25,-10,627392,78,100`,
			expected: &CellInfo{State: "NOCONN", Mode: "NR5G-NSA", MCC: "510", MNC: "11", PCID: 320, RSRP: -78, SINR: 25, RSRQ: -10, EARFCN: 627392, Band: "n78", DLBandwidth: "100", Bandwidth: "100"},
		},
		{
			name:     "WCDMA mode",
			input:    `+QENG: "servingcell","NOCONN","WCDMA",510,11,1234,5678,10562,120,0,-80,-10`,
			expected: &CellInfo{State: "NOCONN", Mode: "WCDMA", MCC: "510", MNC: "11", TAC: "1234", CellID: "5678", EARFCN: 10562, PCID: 120, RSRP: -80},
		},
		{
			name:     "unknown RAT mode",
			input:    `+QENG: "servingcell","NOCONN","UNKNOWN_RAT",510,11,123`,
			expected: &CellInfo{State: "NOCONN", Mode: "UNKNOWN_RAT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseQENGServingCell(tt.input)
			if tt.expected == nil {
				if res != nil {
					t.Fatalf("expected nil, got %+v", res)
				}
				return
			}
			if res == nil {
				t.Fatalf("expected %+v, got nil", tt.expected)
			}
			if res.State != tt.expected.State || res.Mode != tt.expected.Mode {
				t.Errorf("state/mode mismatch: got State=%s Mode=%s; want State=%s Mode=%s", res.State, res.Mode, tt.expected.State, tt.expected.Mode)
			}
			if tt.expected.Band != "" && res.Band != tt.expected.Band {
				t.Errorf("band mismatch: got %s, want %s", res.Band, tt.expected.Band)
			}
		})
	}
}

func TestParseQCAINFO(t *testing.T) {
	raw := `+QCAINFO: "PCC",1675,100,"LTE BAND 3",1,218,-85,-9,-62,18
+QCAINFO: "SCC",300,50,"LTE BAND 1",1,120,-90,-11,-68,14`

	ca := ParseQCAINFO(raw)
	if len(ca) != 2 {
		t.Fatalf("expected 2 CA components, got %d", len(ca))
	}
	if ca[0].Role != "PCC" || ca[0].Band != "B3" {
		t.Errorf("unexpected PCC: %+v", ca[0])
	}
	if ca[1].Role != "SCC" || ca[1].Band != "B1" {
		t.Errorf("unexpected SCC: %+v", ca[1])
	}
	if ca[0].BandwidthMHz != 20 || ca[1].BandwidthMHz != 10 {
		t.Errorf("unexpected bandwidths: PCC=%d SCC=%d", ca[0].BandwidthMHz, ca[1].BandwidthMHz)
	}
}

func TestParseQCAINFO_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "empty input",
			input:    "",
			expected: 0,
		},
		{
			name:     "only OK",
			input:    "OK\r\n",
			expected: 0,
		},
		{
			name: "NR carrier component",
			input: `+QCAINFO: "PCC",627392,100,"NR BAND 78",1,320,-80,-10,-60,20`,
			expected: 1,
		},
		{
			name: "NR carrier with 273 RBs / kHz / MHz",
			input: `+QCAINFO: "PCC",627392,273,"NR BAND 78",1,320,-80,-10,-60,20
+QCAINFO: "SCC",633334,"100MHz","NR BAND 78",1,321,-82,-11,-62,18
+QCAINFO: "SCC",640000,"80000kHz","n77",1,322,-85,-12,-65,15`,
			expected: 3,
		},
		{
			name: "various resource block bandwidths",
			input: `+QCAINFO: "PCC",1675,6,"LTE BAND 3",1,218
+QCAINFO: "SCC",300,15,"LTE BAND 1",1,120
+QCAINFO: "SCC",500,25,"LTE BAND 5",1,130
+QCAINFO: "SCC",600,75,"LTE BAND 7",1,140`,
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseQCAINFO(tt.input)
			if len(res) != tt.expected {
				t.Fatalf("expected %d components, got %d", tt.expected, len(res))
			}
		})
	}
}

func TestParseCSQ(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectNil   bool
		expectedDbm int
	}{
		{
			name:        "standard 24",
			input:       "+CSQ: 24,99\r\nOK",
			expectedDbm: -65,
		},
		{
			name:        "minimum rssi 0",
			input:       "+CSQ: 0,99",
			expectedDbm: -113,
		},
		{
			name:        "rssi 1",
			input:       "+CSQ: 1,99",
			expectedDbm: -111,
		},
		{
			name:        "maximum rssi 31",
			input:       "+CSQ: 31,99",
			expectedDbm: -51,
		},
		{
			name:      "malformed input",
			input:     "ERROR\r\n",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseCSQ(tt.input)
			if tt.expectNil {
				if res != nil {
					t.Fatalf("expected nil, got %+v", res)
				}
				return
			}
			if res == nil {
				t.Fatalf("expected non-nil CSQ, got nil")
			}
			if res.RSRPDbm != tt.expectedDbm {
				t.Errorf("expected RSRPDbm=%d, got %d", tt.expectedDbm, res.RSRPDbm)
			}
		})
	}
}

func TestParseCBC(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectNil   bool
		expectedBCS int
		expectedBCL int
		expectedV   int
	}{
		{
			name:        "valid 3-parameter CBC",
			input:       "+CBC: 0,85,3850\r\nOK",
			expectedBCS: 0,
			expectedBCL: 85,
			expectedV:   3850,
		},
		{
			name:        "valid 2-parameter CBC",
			input:       "+CBC: 1,100\r\nOK",
			expectedBCS: 1,
			expectedBCL: 100,
			expectedV:   0,
		},
		{
			name:        "with URC noise",
			input:       "+CMTI: \"ME\",1\r\n+CBC: 2,90,4100\r\nOK",
			expectedBCS: 2,
			expectedBCL: 90,
			expectedV:   4100,
		},
		{
			name:      "empty input",
			input:     "",
			expectNil: true,
		},
		{
			name:      "invalid line",
			input:     "+CME ERROR: 10\r\n",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseCBC(tt.input)
			if tt.expectNil {
				if res != nil {
					t.Fatalf("expected nil, got %+v", res)
				}
				return
			}
			if res == nil {
				t.Fatalf("expected non-nil CBC, got nil")
			}
			if res.BCS != tt.expectedBCS || res.BCL != tt.expectedBCL || res.Voltage != tt.expectedV {
				t.Errorf("CBC mismatch: got %+v; want BCS=%d BCL=%d V=%d", res, tt.expectedBCS, tt.expectedBCL, tt.expectedV)
			}
		})
	}
}

func TestParseQTEMP(t *testing.T) {
	raw := `+QTEMP: "xo_therm_buf","35"
+QTEMP: "mdm_case_therm","38"
+QTEMP: "pa_therm0","34"`

	info := ParseQTEMP(raw)
	if info == nil {
		t.Fatalf("expected non-nil TemperatureInfo")
	}
	if info.Sensors["xo_therm_buf"] != 35 {
		t.Errorf("expected xo_therm_buf=35, got %d", info.Sensors["xo_therm_buf"])
	}
	if info.Sensors["mdm_case_therm"] != 38 {
		t.Errorf("expected mdm_case_therm=38, got %d", info.Sensors["mdm_case_therm"])
	}
	if info.MaxTemp != 38 {
		t.Errorf("expected MaxTemp=38, got %d", info.MaxTemp)
	}

	// Edge case: invalid/empty
	if ParseQTEMP("") != nil {
		t.Errorf("expected nil for empty QTEMP input")
	}
	if ParseQTEMP("ERROR\r\n") != nil {
		t.Errorf("expected nil for ERROR QTEMP input")
	}
}

func TestFilterURC(t *testing.T) {
	input := "+QIURC: \"recv\",0,4\r\n+CEREG: 1\r\nRING\r\n+CSQ: 20,99\r\nOK\r\n"
	filtered := FilterURC(input)

	if strings.Contains(filtered, "+QIURC:") {
		t.Errorf("URC +QIURC not filtered: %s", filtered)
	}
	if strings.Contains(filtered, "+CEREG:") {
		t.Errorf("URC +CEREG not filtered: %s", filtered)
	}
	if strings.Contains(filtered, "RING") {
		t.Errorf("URC RING not filtered: %s", filtered)
	}
	if !strings.Contains(filtered, "+CSQ: 20,99") || !strings.Contains(filtered, "OK") {
		t.Errorf("expected response content preserved: %s", filtered)
	}
}

func TestParseCSVLine(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`"PCC",1675,100,"LTE BAND 3"`, []string{"PCC", "1675", "100", "LTE BAND 3"}},
		{`510,11,"NOCONN",-85`, []string{"510", "11", "NOCONN", "-85"}},
		{``, []string{""}},
	}

	for _, tt := range tests {
		got := parseCSVLine(tt.input)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("parseCSVLine(%q) = %v; want %v", tt.input, got, tt.expected)
		}
	}
}

func TestParseQENGServingCell_MultilineRG501Q(t *testing.T) {
	raw := `+QENG: "servingcell","NOCONN"
+QENG: "LTE","FDD",510,09,B767015,418,9285,28,4,4,CD85,-104,-13,-74,10,8,200,-
+QENG: "NR5G-NSA",510,09,357,-110,-4,-17,504990,41,7,1

OK`
	info := ParseQENGServingCell(raw)
	if info == nil {
		t.Fatalf("expected non-nil CellInfo")
	}
	if info.State != "NOCONN" {
		t.Errorf("expected State=NOCONN, got %s", info.State)
	}
	if info.Mode != "NR5G-NSA" {
		t.Errorf("expected Mode=NR5G-NSA, got %s", info.Mode)
	}
	if info.Band != "B28" {
		t.Errorf("expected Band=B28, got %s", info.Band)
	}
	if info.PCID != 418 {
		t.Errorf("expected PCID=418, got %d", info.PCID)
	}
	if info.EARFCN != 9285 {
		t.Errorf("expected EARFCN=9285, got %d", info.EARFCN)
	}
	if info.RSRP != -104 || info.RSRQ != -13 || info.SINR != 10 {
		t.Errorf("LTE signal mismatch: RSRP=%d, RSRQ=%d, SINR=%d", info.RSRP, info.RSRQ, info.SINR)
	}
	if !info.HasNR5GNSA {
		t.Errorf("expected HasNR5GNSA=true")
	}
	if info.NR5GBand != "n41" {
		t.Errorf("expected NR5GBand=n41, got %s", info.NR5GBand)
	}
	if info.NR5GARFCN != 504990 {
		t.Errorf("expected NR5GARFCN=504990, got %d", info.NR5GARFCN)
	}
	if info.NR5GPCI != 357 {
		t.Errorf("expected NR5GPCI=357, got %d", info.NR5GPCI)
	}
	if info.NR5GRSRP != -110 || info.NR5GSINR != -4 || info.NR5GRSRQ != -17 {
		t.Errorf("NR signal mismatch: RSRP=%d, SINR=%d, RSRQ=%d", info.NR5GRSRP, info.NR5GSINR, info.NR5GRSRQ)
	}
	if info.NR5GSCS != "30kHz" {
		t.Errorf("expected NR5GSCS=30kHz, got %s", info.NR5GSCS)
	}
}

func TestParseHelperFunctions(t *testing.T) {
	copsRaw := `+COPS: 0,0,"Smartfren Jagoan Sinyal  Smartfren",13`
	if got := ParseCOPS(copsRaw); got != "Smartfren Jagoan Sinyal  Smartfren" {
		t.Errorf("ParseCOPS got %q, want %q", got, "Smartfren Jagoan Sinyal  Smartfren")
	}

	cpinRaw := `+CPIN: READY`
	if got := ParseCPIN(cpinRaw); got != "READY" {
		t.Errorf("ParseCPIN got %q, want %q", got, "READY")
	}

	cgsnRaw := "\r\n869710030002905\r\n\r\nOK\r\n"
	if got := ParseCGSN(cgsnRaw); got != "869710030002905" {
		t.Errorf("ParseCGSN got %q, want %q", got, "869710030002905")
	}

	cimiRaw := "\r\n510283064616102\r\n\r\nOK\r\n"
	if got := ParseCIMI(cimiRaw); got != "510283064616102" {
		t.Errorf("ParseCIMI got %q, want %q", got, "510283064616102")
	}

	qccidRaw := `+QCCID: 89622868003820695237`
	if got := ParseQCCID(qccidRaw); got != "89622868003820695237" {
		t.Errorf("ParseQCCID got %q, want %q", got, "89622868003820695237")
	}

	qrsrpRaw := "+QRSRP: -104,-112,-32768,-32768,LTE\r\n+QRSRP: -98,-108,-115,-95,NR5G\r\n"
	lte, nr := ParseAntennaSignals(qrsrpRaw, "QRSRP")
	if len(lte) != 4 || *lte[0] != -104 || *lte[1] != -112 || lte[2] != nil || lte[3] != nil {
		t.Errorf("ParseAntennaSignals LTE failed: %+v", lte)
	}
	if len(nr) != 4 || *nr[0] != -98 || *nr[1] != -108 || *nr[2] != -115 || *nr[3] != -95 {
		t.Errorf("ParseAntennaSignals NR failed: %+v", nr)
	}

	policyRaw := `+QNWPREFCFG: "gw_band",1:5:6:8
+QNWPREFCFG: "lte_band",1:3:5:7:8:20:28:32:38:40:41:42:43
+QNWPREFCFG: "nsa_nr5g_band",1:3:5:7:8:20:28:38:40:41:77:78
+QNWPREFCFG: "nr5g_band",1:3:5:7:8:20:28:38:40:41:77:78

OK`
	info := ParsePolicyBand(policyRaw)
	if info.LTEBands != "1:3:5:7:8:20:28:32:38:40:41:42:43" {
		t.Errorf("ParsePolicyBand LTE got %q", info.LTEBands)
	}
	if info.NSANR5GBands != "1:3:5:7:8:20:28:38:40:41:77:78" {
		t.Errorf("ParsePolicyBand NSA got %q", info.NSANR5GBands)
	}
	if info.SANR5GBands != "1:3:5:7:8:20:28:38:40:41:77:78" {
		t.Errorf("ParsePolicyBand SA got %q", info.SANR5GBands)
	}
}

func TestParseCFUN(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "CFUN 1",
			input:    "+CFUN: 1\r\nOK",
			expected: 1,
		},
		{
			name:     "CFUN 4",
			input:    "+CFUN: 4\r\nOK",
			expected: 4,
		},
		{
			name:     "CFUN 0",
			input:    "+CFUN: 0\r\nOK",
			expected: 0,
		},
		{
			name:     "invalid",
			input:    "ERROR\r\n",
			expected: 1,
		},
		{
			name:     "missing prefix",
			input:    "OK\r\n",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCFUN(tt.input)
			if got != tt.expected {
				t.Errorf("ParseCFUN(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseTimeAdvance(t *testing.T) {
	// 1. LTE NTA scaling: 1344 (> 1282) / 16 = 84
	rawLTE_NTA := `+QNWCFG: "lte_time_advance",1,1344`
	gotLTE_NTA := ParseTimeAdvance(rawLTE_NTA, false)
	if gotLTE_NTA == nil || *gotLTE_NTA != 84 {
		t.Errorf("expected TA=84 for 1344 NTA, got %v", gotLTE_NTA)
	}

	// 2. LTE standard (<= 1282): 50 -> 50
	rawLTE_Std := `+QNWCFG: "lte_time_advance",1,50`
	gotLTE_Std := ParseTimeAdvance(rawLTE_Std, false)
	if gotLTE_Std == nil || *gotLTE_Std != 50 {
		t.Errorf("expected TA=50, got %v", gotLTE_Std)
	}

	// 3. NR5G: 250 (isNR = true) -> 250 (not divided by 16)
	rawNR := `+QNWCFG: "nr5g_time_advance",1,250`
	gotNR := ParseTimeAdvance(rawNR, true)
	if gotNR == nil || *gotNR != 250 {
		t.Errorf("expected TA=250 for NR, got %v", gotNR)
	}

	// 4. Non-matching or malformed -> returns nil
	if got := ParseTimeAdvance("OK", false); got != nil {
		t.Errorf("expected nil for non-matching line, got %v", got)
	}
	if got := ParseTimeAdvance(`+QNWCFG: "lte_time_advance",1,0`, false); got != nil {
		t.Errorf("expected nil for 0 value, got %v", got)
	}
	if got := ParseTimeAdvance(`+QNWCFG: "lte_time_advance",1,abc`, false); got != nil {
		t.Errorf("expected nil for non-numeric value, got %v", got)
	}
}

func TestParseQCCID(t *testing.T) {
	// 1. Standard +QCCID: prefix
	raw1 := "+QCCID: 89860401101903000000\r\nOK"
	if got := ParseQCCID(raw1); got != "89860401101903000000" {
		t.Errorf("ParseQCCID with prefix got %q, want %q", got, "89860401101903000000")
	}

	// 2. Standalone line with 19-20 digits
	raw2 := "89860401101903000000\r\nOK"
	if got := ParseQCCID(raw2); got != "89860401101903000000" {
		t.Errorf("ParseQCCID standalone got %q, want %q", got, "89860401101903000000")
	}

	// 3. Empty and non-matching lines
	if got := ParseQCCID(""); got != "" {
		t.Errorf("expected empty string for empty input, got %q", got)
	}
	if got := ParseQCCID("OK\r\nERROR\r\n"); got != "" {
		t.Errorf("expected empty string for non-matching input, got %q", got)
	}
	if got := ParseQCCID("AT+QCCID\r\nOK"); got != "" {
		t.Errorf("expected empty string for command echo, got %q", got)
	}
}

func TestParseBandwidthMHz(t *testing.T) {
	// 1. LTE bw codes: "0", "1", "2", "3", "4", "5"
	lteCodeTests := []struct {
		code     string
		expected int
	}{
		{"0", 1},
		{"1", 3},
		{"2", 5},
		{"3", 10},
		{"4", 15},
		{"5", 20},
	}
	for _, tt := range lteCodeTests {
		t.Run("LTE_Code_"+tt.code, func(t *testing.T) {
			got := ParseBandwidthMHz(tt.code, "LTE")
			if got != tt.expected {
				t.Errorf("ParseBandwidthMHz(%q, LTE) = %d, want %d", tt.code, got, tt.expected)
			}
		})
	}

	// 2. NR bw codes and RBs
	nrTests := []struct {
		input    string
		expected int
	}{
		{"273", 100},
		{"217", 80},
		{"216", 80},
		{"162", 60},
		{"135", 50},
		{"106", 40},
		{"79", 15},
		{"51", 20},
		{"25", 10},
		{"100MHz", 100},
		{"80000kHz", 80},
		{"20M", 20},
	}
	for _, tt := range nrTests {
		t.Run("NR_"+tt.input, func(t *testing.T) {
			got := ParseBandwidthMHz(tt.input, "NR")
			if got != tt.expected {
				t.Errorf("ParseBandwidthMHz(%q, NR) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}

	// 3. Unknown/invalid inputs
	invalidTests := []struct {
		input string
		tech  string
	}{
		{"", "LTE"},
		{"", "NR"},
		{"invalid", "LTE"},
		{"-10", "NR"},
	}
	for _, tt := range invalidTests {
		got := ParseBandwidthMHz(tt.input, tt.tech)
		if got != 0 {
			t.Errorf("ParseBandwidthMHz(%q, %s) = %d, want 0", tt.input, tt.tech, got)
		}
	}
}

