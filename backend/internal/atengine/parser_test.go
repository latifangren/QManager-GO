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
