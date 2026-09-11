package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

func TestSMSForwarder_Helpers(t *testing.T) {
	// 1. Phone number normalization
	if NormalizePhoneNumber("08123456789", "62") != "+628123456789" {
		t.Errorf("expected +628123456789, got %s", NormalizePhoneNumber("08123456789", "62"))
	}
	if NormalizePhoneNumber("00628123456789", "") != "+628123456789" {
		t.Errorf("expected +628123456789, got %s", NormalizePhoneNumber("00628123456789", ""))
	}
	if NormalizePhoneNumber("+1234567890", "") != "+1234567890" {
		t.Errorf("expected +1234567890, got %s", NormalizePhoneNumber("+1234567890", ""))
	}
	if NormalizePhoneNumber("", "62") != "" {
		t.Errorf("expected empty string for empty phone")
	}

	// 2. ValidateTargetPhone
	if !ValidateTargetPhone("+628123456789") {
		t.Errorf("expected valid for +628123456789")
	}
	if ValidateTargetPhone("08123456789") {
		t.Errorf("expected invalid for phone starting with 0 without plus")
	}
	if ValidateTargetPhone("123") {
		t.Errorf("expected invalid for short phone number")
	}
	if ValidateTargetPhone("+12345abcde") {
		t.Errorf("expected invalid for non-digit phone number")
	}

	// 3. FormatForwardSMS
	formatted := FormatForwardSMS("+1234567890", "Verification code 9876")
	if formatted != "From +1234567890: Verification code 9876" {
		t.Errorf("unexpected FormatForwardSMS output: %s", formatted)
	}

	// 4. IsRelayMessage
	if !IsRelayMessage("From +1234567890: Verification code 9876") {
		t.Errorf("expected true for relay message")
	}
	if !IsRelayMessage("From 1234567890: Verification code 9876") {
		t.Errorf("expected true for relay message without plus")
	}
	if IsRelayMessage("Hello from user") {
		t.Errorf("expected false for standard message")
	}
	if IsRelayMessage("From : short") {
		t.Errorf("expected false for empty token")
	}

	// 5. DJB2Fingerprint
	fp1 := DJB2Fingerprint("ME", "+1234", "24/05/10 10:00:00", "Test")
	fp2 := DJB2Fingerprint("ME", "+1234", "24/05/10 10:00:00", "Test")
	fp3 := DJB2Fingerprint("SM", "+1234", "24/05/10 10:00:00", "Test")
	if fp1 != fp2 {
		t.Errorf("expected deterministic hash")
	}
	if fp1 == fp3 {
		t.Errorf("expected different hash for different storage")
	}

	// 6. MatchesKeywordFilter
	if !MatchesKeywordFilter("Your OTP is 123456", "otp, token, verify") {
		t.Errorf("expected match for 'otp'")
	}
	if MatchesKeywordFilter("Hello world", "bank, alert") {
		t.Errorf("expected no match for 'bank, alert'")
	}
	if !MatchesKeywordFilter("Payment received $500", "regex:^payment.*\\$500") {
		t.Errorf("expected match for regex")
	}
	if !MatchesKeywordFilter("Any message", "") {
		t.Errorf("expected match for empty filter")
	}
}

func TestSMSForwarder_EvaluateForwardingRules(t *testing.T) {
	msg := SMSMessage{
		Sender:    "BankAlert",
		Content:   "Your one-time passcode is 445566",
		Timestamp: "24/06/01 12:00:00",
		Storage:   "ME",
	}

	rules := []SMSForwardingRule{
		{
			ID:             "rule1",
			Name:           "Bank Rule",
			Enabled:        true,
			MatchSender:    "BankAlert",
			MatchKeyword:   "passcode",
			TargetType:     "webhook",
			TargetEndpoint: "https://webhook.site/test",
			CustomTemplate: "Alert from {sender} at {timestamp}: {content}",
		},
		{
			ID:           "rule2",
			Name:         "Disabled Rule",
			Enabled:      false,
			MatchSender:  "BankAlert",
			MatchKeyword: "passcode",
		},
		{
			ID:           "rule3",
			Name:         "Keyword mismatch",
			Enabled:      true,
			MatchSender:  "BankAlert",
			MatchKeyword: "transaction",
		},
		{
			ID:           "rule4",
			Name:         "Regex Sender",
			Enabled:      true,
			MatchSender:  "Bank.*",
			MatchKeyword: "passcode",
		},
	}

	matched := EvaluateForwardingRules(msg, rules)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched rules, got %d", len(matched))
	}
	if matched[0].ID != "rule1" || matched[1].ID != "rule4" {
		t.Errorf("unexpected matched rule IDs: %+v", matched)
	}
}

func TestSMSForwarder_WebhookDispatch(t *testing.T) {
	var receivedPayload map[string]interface{}
	var receivedUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(confPath)

	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)

	forwarder := NewSMSForwarder(eng, cfgMgr)
	msg := SMSMessage{
		Sender:    "+62811223344",
		Content:   "Hello Webhook",
		Timestamp: "24/05/10 14:00:00",
		Storage:   "ME",
	}

	err := forwarder.sendWebhook(context.Background(), msg, server.URL)
	if err != nil {
		t.Fatalf("sendWebhook failed: %v", err)
	}

	if receivedUserAgent != "QManager-SMSForwarder/1.0" {
		t.Errorf("expected User-Agent QManager-SMSForwarder/1.0, got %s", receivedUserAgent)
	}
	if receivedPayload["sender"] != "+62811223344" || receivedPayload["content"] != "Hello Webhook" {
		t.Errorf("unexpected payload received: %+v", receivedPayload)
	}

	// Test webhook error response
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	err = forwarder.sendWebhook(context.Background(), msg, errorServer.URL)
	if err == nil {
		t.Errorf("expected error when webhook returns 500, got nil")
	}
}

func TestSMSForwarder_RunCycleWithMockServer(t *testing.T) {
	var webhookHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "sms_forwarding.json")
	failPath := filepath.Join(tmpDir, "sms_failures.json")
	seenPath := filepath.Join(tmpDir, "sms_seen")

	cfg := SMSForwardingSettings{
		Enabled:        true,
		WebhookEnabled: true,
		WebhookURL:     server.URL,
		Rules: []SMSForwardingRule{
			{
				ID:             "r1",
				Enabled:        true,
				MatchSender:    "+123456",
				TargetType:     "webhook",
				TargetEndpoint: server.URL,
				CustomTemplate: "Formatted: {content}",
			},
		},
	}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, data, 0644)

	t.Setenv("SMS_FORWARD_CONFIG_PATH", cfgPath)
	t.Setenv("SMS_FORWARD_FAILURES_PATH", failPath)
	t.Setenv("SMS_FORWARD_SEEN_PATH", seenPath)
	t.Setenv("SMS_TOOL_PATH", "/non/existent/sms_tool")

	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CREG?", "+CREG: 0,1")
	mock.SetResponse(`AT+CPMS="ME","ME","ME"`, "+CPMS: 1,255,1,255,0,255\nOK")
	mock.SetResponse("AT+CPMS?", "+CPMS: 1,255,1,255,0,255\nOK")
	mock.SetResponse(`AT+CPMS="SM","SM","SM"`, "+CPMS: 0,255,0,255,0,255\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse(`AT+CMGL="ALL"`, "+CMGL: 1,\"REC READ\",\"+123456\",,\"24/05/10 12:00:00+00\"\nTest verification message\nOK")

	eng := atengine.NewEngine(mock)
	cfgMgr, _ := config.NewManager(filepath.Join(tmpDir, "qmanager.conf"))

	forwarder := NewSMSForwarder(eng, cfgMgr)

	// Seed seen set
	forwarder.runCycle(true)
	if !forwarder.seenFileExists() {
		t.Fatalf("expected seen file to exist after seed")
	}

	// Add second message to trigger forward
	mock.SetResponse(`AT+CMGL="ALL"`, "+CMGL: 1,\"REC READ\",\"+123456\",,\"24/05/10 12:00:00+00\"\nTest verification message\n+CMGL: 2,\"REC UNREAD\",\"+123456\",,\"24/05/10 12:05:00+00\"\nSecond message\nOK")

	forwarder.runCycle(false)
	if !webhookHit {
		t.Errorf("expected webhook to be invoked on forward cycle")
	}

	// Test Start and Stop loop
	forwarder.Start()
	forwarder.Start() // Idempotent start
	time.Sleep(50 * time.Millisecond)
	forwarder.Stop()
	forwarder.Stop() // Idempotent stop
}

func TestSMSForwarder_ParseCMGL_And_Storage(t *testing.T) {
	rawCPMS := `+CPMS: "ME",5,255,"SM",2,50,"ME",5,255
OK`
	me, sm := ParseCPMSStorage(rawCPMS)
	if me.Used != 5 || me.Total != 255 {
		t.Errorf("expected ME (5, 255), got (%d, %d)", me.Used, me.Total)
	}
	if sm.Used != 2 || sm.Total != 50 {
		t.Errorf("expected SM (2, 50), got (%d, %d)", sm.Used, sm.Total)
	}

	rawCMGL := `+CMGL: 1,"REC READ","+1234567890",,"24/01/01 10:00:00+00"
First test message
+CMGL: 2,"REC UNREAD","+9876543210",,"24/01/02 11:00:00+00"
Second test message
`
	msgs := ParseCMGLText(rawCMGL, "ME")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 parsed messages, got %d", len(msgs))
	}
	if msgs[0].Sender != "+1234567890" || msgs[0].Content != "First test message" {
		t.Errorf("unexpected message 0: %+v", msgs[0])
	}
	if msgs[1].Sender != "+9876543210" || msgs[1].Content != "Second test message" {
		t.Errorf("unexpected message 1: %+v", msgs[1])
	}

	SortSMSMessages(msgs)
	if msgs[0].Timestamp < msgs[1].Timestamp {
		t.Errorf("expected newest message first after sort")
	}
}

func TestSMSForwarder_RecordFailure(t *testing.T) {
	tmpDir := t.TempDir()
	failPath := filepath.Join(tmpDir, "failures.json")
	t.Setenv("SMS_FORWARD_FAILURES_PATH", failPath)

	forwarder := NewSMSForwarder(nil, nil)
	forwarder.recordFailure("+123456", "network timeout")

	data, err := os.ReadFile(failPath)
	if err != nil {
		t.Fatalf("failed to read failure file: %v", err)
	}

	var failures []SMSForwardingFailure
	if err := json.Unmarshal(data, &failures); err != nil {
		t.Fatalf("invalid json in failure file: %v", err)
	}

	if len(failures) != 1 || failures[0].Sender != "+123456" || failures[0].Error != "network timeout" {
		t.Errorf("unexpected failure file contents: %+v", failures)
	}
}

func TestSMSForwarder_HelpersAndParsing(t *testing.T) {
	// 1. ParseSmsToolStatusOutput
	stat := ParseSmsToolStatusOutput("Storage type: ME, used: 4, total: 255")
	if stat.Used != 4 || stat.Total != 255 {
		t.Errorf("ParseSmsToolStatusOutput mismatch: %+v", stat)
	}

	// 2. ConvertRawSmsItems - single and multipart
	rawItems := []RawSmsToolItem{
		{Index: 1, Sender: "+6281", Content: "Single part msg", Timestamp: "2026/08/30 10:00:00"},
		{Index: 2, Sender: "+6282", Text: "Part 1", Reference: 100, Total: 2, Part: 1, Timestamp: "2026/08/30 10:01:00"},
		{Index: 3, Sender: "+6282", Text: "Part 2", Reference: 100, Total: 2, Part: 2, Timestamp: "2026/08/30 10:01:00"},
	}
	converted := ConvertRawSmsItems(rawItems, "ME")
	if len(converted) != 2 {
		t.Fatalf("expected 2 converted messages (1 single + 1 multipart), got %d", len(converted))
	}

	// 3. extractIndexes
	idxs := extractIndexes(5)
	if len(idxs) != 1 || idxs[0] != 5 {
		t.Errorf("extractIndexes mismatch: %v", idxs)
	}

	// 4. parseTimestampKey
	k1 := parseTimestampKey("2026/08/30,12:00:00")
	k2 := parseTimestampKey("2026/08/30 12:00:00")
	if k1 == "" || k2 == "" {
		t.Errorf("parseTimestampKey failed: %s, %s", k1, k2)
	}
}

func TestSMSForwarder_FetchInboxAndSendRetry(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CPMS?", `+CPMS: "ME",1,255,"SM",0,50,"ME",1,255`+"\r\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CSCS=\"GSM\"", "OK")
	mock.SetResponse("AT+CPMS=\"ME\",\"ME\",\"ME\"", "OK")
	mock.SetResponse("AT+CPMS=\"SM\",\"SM\",\"SM\"", "OK")
	mock.SetResponse(`AT+CMGL="ALL"`, `+CMGL: 1,"REC UNREAD","+62811111111",,"2026/08/30 12:00:00+28"
Forward this message
OK`)

	eng := atengine.NewEngine(mock)
	defer eng.Close()

	// FetchInboxAndStorage
	ctx := context.Background()
	msgs, storage, err := FetchInboxAndStorage(ctx, "", "", eng)
	if err != nil {
		t.Fatalf("FetchInboxAndStorage failed: %v", err)
	}
	if len(msgs) == 0 || storage.Total != 305 {
		t.Errorf("unexpected inbox results: len=%d storage=%+v", len(msgs), storage)
	}

	cfgPath := filepath.Join(t.TempDir(), "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)
	forwarder := NewSMSForwarder(eng, cfgMgr)

	// sendSMSWithRetry
	mock.SetResponse("AT+CREG?", "+CREG: 0,1\r\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CMGS=\"+62899999999\"\rTest text message\x1A", "+CMGS: 100\r\nOK")
	err = forwarder.sendSMSWithRetry(ctx, "+62899999999", "Test text message")
	if err != nil {
		t.Errorf("sendSMSWithRetry failed: %v", err)
	}
}
