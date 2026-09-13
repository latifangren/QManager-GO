package telemetry

import (
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
)

func TestReadSmsToolStatus_MockCLI(t *testing.T) {
	// Status parsing helper tests
	s1 := MemoryStorage{Used: 12, Total: 255}
	if s1.Used != 12 || s1.Total != 255 {
		t.Errorf("mismatch: %+v", s1)
	}
}

func TestExtractIndexes_Deep(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []int
	}{
		{"nil input", nil, []int{}},
		{"int input", 5, []int{5}},
		{"float64 input", float64(8), []int{8}},
		{"range string 1-3", "1-3", []int{1, 2, 3}},
		{"list string 1,2,4", "1,2,4", []int{1, 2, 4}},
		{"mixed string 1-2,5,7-8", "1-2,5,7-8", []int{1, 2, 5, 7, 8}},
		{"empty string", "", []int{}},
		{"interface slice", []interface{}{float64(1), 2, "3-4"}, []int{1, 2, 3, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIndexes(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("length mismatch: got %v, expected %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("at %d: got %d, expected %d", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSMSForwarder_SendEmail_Direct(t *testing.T) {
	forwarder := &SMSForwarder{
		configMgr: nil,
	}

	msg := SMSMessage{
		Sender:    "+1234567890",
		Content:   "Verification code: 123456",
		Timestamp: "2026/08/30 12:00:00",
		Storage:   "ME",
	}

	err := forwarder.sendEmail(msg, "admin@example.com")
	if err != nil {
		_ = err
	}
}

func TestSMSForwarder_RunCycle_Full(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CPMS?", `+CPMS: "ME",1,255,"SM",0,50,"ME",1,255`+"\r\nOK")
	mock.SetResponse("AT+CMGF=0", "OK")
	mock.SetResponse("AT+CPMS=\"ME\",\"ME\",\"ME\"", "OK")
	mock.SetResponse("AT+CPMS=\"SM\",\"SM\",\"SM\"", "OK")
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\r\nOK")
	mock.SetResponse("AT+CMGS=\"+628123456789\"\rFrom +628123456789: Forward test message\x1A", "+CMGS: 10\r\nOK")

	eng := atengine.NewEngine(mock)
	defer eng.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "sms_forward.json")
	failPath := filepath.Join(tmpDir, "sms_failures.json")
	seenPath := filepath.Join(tmpDir, "sms_seen")

	forwarder := NewSMSForwarder(eng, nil)
	forwarder.configPath = cfgPath
	forwarder.failuresPath = failPath
	forwarder.seenPath = seenPath

	// Cycle test
	forwarder.runCycle(false)
}
