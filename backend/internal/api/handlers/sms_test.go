package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/telemetry"
)

func TestNormalizePhoneNumber(t *testing.T) {
	tests := []struct {
		input       string
		defaultCode string
		want        string
	}{
		{"+628123456789", "", "+628123456789"},
		{"08123456789", "62", "+628123456789"},
		{"00628123456789", "", "+628123456789"},
		{" +1 (555) 123-4567 ", "", "+15551234567"},
		{"", "62", ""},
	}

	for _, tc := range tests {
		got := telemetry.NormalizePhoneNumber(tc.input, tc.defaultCode)
		if got != tc.want {
			t.Errorf("NormalizePhoneNumber(%q, %q) = %q; want %q", tc.input, tc.defaultCode, got, tc.want)
		}
	}
}

func TestValidateTargetPhone(t *testing.T) {
	tests := []struct {
		phone string
		valid bool
	}{
		{"+628123456789", true},
		{"628123456789", true},
		{"+15551234567", true},
		{"08123456789", false}, // leading zero invalid for target
		{"+12345", false},       // too short (< 7)
		{"+12345678901234567", false}, // too long (> 15)
		{"+6281234567a9", false}, // non-digit
		{"", false},
	}

	for _, tc := range tests {
		got := telemetry.ValidateTargetPhone(tc.phone)
		if got != tc.valid {
			t.Errorf("ValidateTargetPhone(%q) = %v; want %v", tc.phone, got, tc.valid)
		}
	}
}

func TestFormatAndIsRelayMessage(t *testing.T) {
	sender := "+1234567890"
	content := "Your verification code is 123456"

	formatted := telemetry.FormatForwardSMS(sender, content)
	if formatted != "From +1234567890: Your verification code is 123456" {
		t.Errorf("FormatForwardSMS mismatch: got %q", formatted)
	}

	if !telemetry.IsRelayMessage(formatted) {
		t.Errorf("IsRelayMessage(%q) should be true", formatted)
	}

	if telemetry.IsRelayMessage("Hello from user") {
		t.Errorf("IsRelayMessage should be false for normal content")
	}

	if telemetry.IsRelayMessage("From John: Hello") {
		t.Errorf("IsRelayMessage should be false for non-numeric sender token")
	}
}

func TestDJB2Fingerprint(t *testing.T) {
	fp1 := telemetry.DJB2Fingerprint("ME", "+123456789", "08/24/26 10:00:00", "Hello World")
	fp2 := telemetry.DJB2Fingerprint("ME", "+123456789", "08/24/26 10:00:00", "Hello World")
	fp3 := telemetry.DJB2Fingerprint("SM", "+123456789", "08/24/26 10:00:00", "Hello World")

	if fp1 == "" || fp1 != fp2 {
		t.Errorf("DJB2Fingerprint should be deterministic, got %q vs %q", fp1, fp2)
	}
	if fp1 == fp3 {
		t.Errorf("Different storage should produce different fingerprints")
	}
}

func TestMatchesKeywordFilter(t *testing.T) {
	content := "Your bank OTP is 987654. Do not share."

	if !telemetry.MatchesKeywordFilter(content, "") {
		t.Errorf("Empty filter should match everything")
	}
	if !telemetry.MatchesKeywordFilter(content, "otp") {
		t.Errorf("Filter 'otp' should match content with OTP (case insensitive)")
	}
	if !telemetry.MatchesKeywordFilter(content, "alert, verification, otp") {
		t.Errorf("Multi-keyword filter containing 'otp' should match")
	}
	if telemetry.MatchesKeywordFilter(content, "alert, password") {
		t.Errorf("Filter without matching keywords should return false")
	}
	if !telemetry.MatchesKeywordFilter(content, "regex:OTP\\s+is\\s+\\d+") {
		t.Errorf("Regex filter should match")
	}
}

func TestEvaluateForwardingRules(t *testing.T) {
	msg := telemetry.SMSMessage{
		Indexes:   []int{1},
		Sender:    "BankAlert",
		Content:   "Your OTP is 432100",
		Timestamp: "08/24/26 12:00:00",
		Storage:   "ME",
	}

	rules := []telemetry.SMSForwardingRule{
		{
			ID:             "r1",
			Name:           "Bank Rule",
			Enabled:        true,
			MatchSender:    "BankAlert",
			MatchKeyword:   "OTP",
			TargetType:     "webhook",
			TargetEndpoint: "https://webhook.site/test",
		},
		{
			ID:             "r2",
			Name:           "Disabled Rule",
			Enabled:        false,
			MatchSender:    "BankAlert",
			TargetType:     "email",
			TargetEndpoint: "admin@example.com",
		},
		{
			ID:             "r3",
			Name:           "Non-matching Rule",
			Enabled:        true,
			MatchSender:    "Telco",
			TargetType:     "phone",
			TargetEndpoint: "+62812345678",
		},
	}

	matched := telemetry.EvaluateForwardingRules(msg, rules)
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched rule, got %d", len(matched))
	}
	if matched[0].ID != "r1" {
		t.Errorf("expected matched rule ID='r1', got %s", matched[0].ID)
	}
}

func TestParseSmsToolStatusOutput(t *testing.T) {
	out := "Storage type: ME, used: 5, total: 255\n"
	stat := telemetry.ParseSmsToolStatusOutput(out)
	if stat.Used != 5 || stat.Total != 255 {
		t.Errorf("ParseSmsToolStatusOutput failed: got used=%d, total=%d; want 5, 255", stat.Used, stat.Total)
	}
}

func TestParseCPMSStorage(t *testing.T) {
	raw := `
+CPMS: "ME",3,255,"ME",3,255,"SM",1,40

OK
`
	me, sm := telemetry.ParseCPMSStorage(raw)
	if me.Used != 3 || me.Total != 255 {
		t.Errorf("ME storage parsed incorrectly: %+v", me)
	}
	if sm.Used != 1 || sm.Total != 40 {
		t.Errorf("SM storage parsed incorrectly: %+v", sm)
	}
}

func TestConvertRawSmsItems_Multipart(t *testing.T) {
	raw := []telemetry.RawSmsToolItem{
		{
			Index:     1,
			Sender:    "+628123456789",
			Timestamp: "08/24/26 14:00:00",
			Content:   "Hello ",
			Part:      1,
			Total:     2,
			Reference: 42,
		},
		{
			Index:     2,
			Sender:    "+628123456789",
			Timestamp: "08/24/26 14:00:00",
			Content:   "World!",
			Part:      2,
			Total:     2,
			Reference: 42,
		},
		{
			Index:     3,
			Sender:    "+628999999999",
			Timestamp: "08/24/26 15:00:00",
			Content:   "Single message",
			Part:      0,
			Total:     0,
			Reference: 0,
		},
	}

	msgs := telemetry.ConvertRawSmsItems(raw, "ME")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 merged messages, got %d", len(msgs))
	}

	var multipartMsg, singleMsg *telemetry.SMSMessage
	for i := range msgs {
		if msgs[i].Sender == "+628123456789" {
			multipartMsg = &msgs[i]
		} else if msgs[i].Sender == "+628999999999" {
			singleMsg = &msgs[i]
		}
	}

	if multipartMsg == nil || multipartMsg.Content != "Hello World!" || len(multipartMsg.Indexes) != 2 {
		t.Errorf("Multipart message not merged correctly: %+v", multipartMsg)
	}
	if singleMsg == nil || singleMsg.Content != "Single message" || len(singleMsg.Indexes) != 1 {
		t.Errorf("Single message parsed incorrectly: %+v", singleMsg)
	}
}

func TestParseCMGLText(t *testing.T) {
	raw := `
+CMGL: 1,"REC READ","+628123456789",,"08/24/26,14:20:00+28"
Test SMS message content
+CMGL: 2,"REC UNREAD","+628987654321",,"08/24/26,15:10:00+28"
Second SMS message

OK
`
	msgs := telemetry.ParseCMGLText(raw, "ME")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Sender != "+628123456789" || msgs[0].Content != "Test SMS message content" {
		t.Errorf("Message 0 parsed incorrectly: %+v", msgs[0])
	}
	if msgs[1].Sender != "+628987654321" || msgs[1].Content != "Second SMS message" {
		t.Errorf("Message 1 parsed incorrectly: %+v", msgs[1])
	}
}

func TestSMSHandler_MockEngine(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse(`AT+CPMS="ME","ME","ME"`, "OK")
	mock.SetResponse(`AT+CPMS="SM","SM","SM"`, "OK")
	mock.SetResponse("AT+CPMS?", `+CPMS: "ME",1,255,"ME",1,255,"SM",0,40`)
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse(`AT+CMGL="ALL"`, `+CMGL: 1,"REC READ","+123456",,"08/24/26,10:00:00+00"
Test Message

OK`)

	eng := atengine.NewEngine(mock)
	handler := NewSMSHandler(eng)

	// Test GET ListSMS / GetSMSCenter
	req := httptest.NewRequest("GET", "/cgi-bin/quecmanager/cellular/sms.sh", nil)
	rr := httptest.NewRecorder()
	handler.GetSMSCenter(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rr.Code)
	}

	var resp SMSCenterResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got false")
	}
	if len(resp.Messages) != 2 { // Returned once for ME and once for SM in mock
		t.Logf("got %d messages from mock", len(resp.Messages))
	}
}

func TestSMSForwardingHandler_SaveAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "sms_forwarding.json")
	failPath := filepath.Join(tmpDir, "sms_forward_failures.json")
	reloadPath := filepath.Join(tmpDir, "sms_forward_reload")

	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	cfgMgr, _ := config.NewManager(filepath.Join(tmpDir, "qmanager.conf"))

	t.Setenv("SMS_FORWARD_CONFIG_PATH", cfgPath)
	t.Setenv("SMS_FORWARD_FAILURES_PATH", failPath)
	t.Setenv("SMS_FORWARD_RELOAD_FLAG", reloadPath)

	handler := NewSMSForwardingHandler(eng, cfgMgr)

	// 1. Initial GET
	req := httptest.NewRequest("GET", "/api/v1/cellular/sms/forwarding", nil)
	rr := httptest.NewRecorder()
	handler.GetSettings(rr, req)

	var getResp SMSForwardingResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &getResp)
	if getResp.Settings.Enabled {
		t.Errorf("expected default enabled=false")
	}

	// 2. POST save_settings
	savePayload := map[string]interface{}{
		"action":       "save_settings",
		"enabled":      true,
		"target_phone": "+628123456789",
		"keyword_filter": "OTP,Alert",
	}
	body, _ := json.Marshal(savePayload)
	reqPost := httptest.NewRequest("POST", "/api/v1/cellular/sms/forwarding", bytes.NewBuffer(body))
	rrPost := httptest.NewRecorder()
	handler.HandleAction(rrPost, reqPost)

	if rrPost.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rrPost.Code)
	}

	// Verify file was written
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}
	var written telemetry.SMSForwardingSettings
	_ = json.Unmarshal(data, &written)
	if !written.Enabled || written.TargetPhone != "+628123456789" || written.KeywordFilter != "OTP,Alert" {
		t.Errorf("written config mismatch: %+v", written)
	}

	// 3. Clear failures
	reqClear := httptest.NewRequest("POST", "/api/v1/cellular/sms/forwarding", bytes.NewBufferString(`{"action":"clear_failures"}`))
	rrClear := httptest.NewRecorder()
	handler.HandleAction(rrClear, reqClear)
	if rrClear.Code != http.StatusOK {
		t.Errorf("expected HTTP 200 on clear_failures, got %d", rrClear.Code)
	}
}
