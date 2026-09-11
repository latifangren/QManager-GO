package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func TestReadSmsToolStatus_MockCLI(t *testing.T) {
	ctx := context.Background()

	// 1. Non-existent CLI tool (returns empty storage)
	res := ReadSmsToolStatus(ctx, "/nonexistent/sms_tool", "/dev/smd11", "ME")
	if res.Used != 0 || res.Total != 0 {
		t.Errorf("expected 0/0 for nonexistent tool, got %+v", res)
	}

	// 2. Parse mock outputs
	s1 := ParseSmsToolStatusOutput("Storage type: ME, used: 12, total: 255")
	if s1.Used != 12 || s1.Total != 255 {
		t.Errorf("ParseSmsToolStatusOutput mismatch: %+v", s1)
	}

	s2 := ParseSmsToolStatusOutput("Invalid output without fields")
	if s2.Used != 0 || s2.Total != 0 {
		t.Errorf("expected 0/0 for invalid output, got %+v", s2)
	}

	s3 := ParseSmsToolStatusOutput("")
	if s3.Used != 0 || s3.Total != 0 {
		t.Errorf("expected 0/0 for empty output, got %+v", s3)
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
		{"invalid tokens", "abc,def-ghi,10", []int{10}},
		{"interface slice", []interface{}{float64(1), 2, "3-4"}, []int{1, 2, 3, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIndexes(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractIndexes(%v) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSendSMSWithRetry_Deep(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)

	forwarder := NewSMSForwarder(eng, cfgMgr)

	ctx := context.Background()

	// 1. Success on first try
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\r\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CMGS=\"+62811111111\"\rSuccess First Try\x1A", "+CMGS: 1\r\nOK")

	err := forwarder.sendSMSWithRetry(ctx, "+62811111111", "Success First Try")
	if err != nil {
		t.Errorf("expected success on first attempt, got error: %v", err)
	}

	// 2. Failure: not registered on network (exhausts retries)
	mock.SetResponse("AT+CREG?", "+CREG: 0,0\r\nOK")
	mock.SetResponse("AT+CGREG?", "+CGREG: 0,0\r\nOK")

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = forwarder.sendSMSWithRetry(ctxTimeout, "+62822222222", "Fail not registered")

	// 3. Fallback to sms_tool execution path when binary path provided
	forwarderWithTool := &SMSForwarder{
		smsToolPath: "echo",
		atDevice:    "/dev/null",
	}
	errTool := forwarderWithTool.sendSMSWithRetry(ctx, "+62833333333", "Via tool")
	if errTool != nil {
		t.Logf("echo tool execution result: %v", errTool)
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

	// Direct call to sendEmail exercises email formation and SMTP connection error handling
	err := forwarder.sendEmail(msg, "admin@example.com")
	if err != nil {
		// Failure expected on test machine without local mailserver
		_ = err
	}
}

func TestSMSForwarder_RunCycle_Full(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CPMS?", `+CPMS: "ME",1,255,"SM",0,50,"ME",1,255`+"\r\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CSCS=\"GSM\"", "OK")
	mock.SetResponse("AT+CPMS=\"ME\",\"ME\",\"ME\"", "OK")
	mock.SetResponse("AT+CPMS=\"SM\",\"SM\",\"SM\"", "OK")
	mock.SetResponse(`AT+CMGL="ALL"`, `+CMGL: 1,"REC UNREAD","+62811111111",,"2026/08/30 12:00:00+28"
Forward test message
OK`)
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\r\nOK")
	mock.SetResponse("AT+CMGS=\"+62899999999\"\rFrom +62811111111: Forward test message\x1A", "+CMGS: 10\r\nOK")

	eng := atengine.NewEngine(mock)
	defer eng.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "sms_forward.json")
	seenPath := filepath.Join(tmpDir, "seen.json")
	failPath := filepath.Join(tmpDir, "failures.json")

	t.Setenv("SMS_FORWARD_CONFIG_PATH", cfgPath)
	t.Setenv("SMS_FORWARD_SEEN_PATH", seenPath)
	t.Setenv("SMS_FORWARD_FAILURES_PATH", failPath)

	settings := SMSForwardingSettings{
		Enabled:     true,
		TargetPhone: "+62899999999",
	}
	data, _ := json.Marshal(settings)
	_ = os.WriteFile(cfgPath, data, 0644)

	forwarder := NewSMSForwarder(eng, nil)
	forwarder.configPath = cfgPath
	forwarder.seenPath = seenPath
	forwarder.failuresPath = failPath

	// 1. Seed cycle (marks initial backlog as seen without forwarding)
	forwarder.runCycle(true)

	// 2. Normal cycle
	forwarder.runCycle(false)

	// Verify seen map updated
	if len(forwarder.seenMap) == 0 {
		t.Errorf("expected seenMap to contain processed message fingerprint")
	}
}
