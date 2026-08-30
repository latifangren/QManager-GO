package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

const (
	DefaultSMSToolPath         = "/usr/bin/sms_tool"
	DefaultSMSATDevice         = "/dev/smd11"
	DefaultSMSForwardConfig    = "/etc/qmanager/sms_forwarding.json"
	DefaultSMSForwardFailures  = "/tmp/qmanager_sms_forward_failures.json"
	DefaultSMSForwardSeen      = "/tmp/qmanager_sms_forward_seen"
	DefaultSMSForwardReload    = "/tmp/qmanager_sms_forward_reload"
	DefaultSMSForwardUnitName  = "qmanager-sms-forward.service"
	DefaultSMSPollInterval     = 15 * time.Second
	MaxSMSForwardFailures      = 20
)

// SMSMessage represents a single or concatenated SMS message.
type SMSMessage struct {
	Indexes   []int  `json:"indexes"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Storage   string `json:"storage"` // "ME" or "SM"
}

// MemoryStorage represents storage usage for one memory area.
type MemoryStorage struct {
	Used  int `json:"used"`
	Total int `json:"total"`
}

// SMSStorage represents combined and individual storage stats.
type SMSStorage struct {
	Used  int            `json:"used"`
	Total int            `json:"total"`
	ME    *MemoryStorage `json:"me,omitempty"`
	SM    *MemoryStorage `json:"sm,omitempty"`
}

// SMSForwardingRule defines an advanced conditional forwarding rule.
type SMSForwardingRule struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	MatchSender    string `json:"match_sender,omitempty"`    // Exact sender or regex
	MatchKeyword   string `json:"match_keyword,omitempty"`   // Keyword substring or regex
	TargetType     string `json:"target_type"`               // "phone", "email", "webhook"
	TargetEndpoint string `json:"target_endpoint"`           // Phone number, Email, Webhook URL
	CustomTemplate string `json:"custom_template,omitempty"` // Template with {sender}, {content}, {timestamp}
}

// SMSForwardingSettings holds the configuration stored in /etc/qmanager/sms_forwarding.json.
type SMSForwardingSettings struct {
	Enabled        bool                `json:"enabled"`
	TargetPhone    string              `json:"target_phone"`
	EmailEnabled   bool                `json:"email_enabled,omitempty"`
	EmailAddress   string              `json:"email_address,omitempty"`
	WebhookEnabled bool                `json:"webhook_enabled,omitempty"`
	WebhookURL     string              `json:"webhook_url,omitempty"`
	KeywordFilter  string              `json:"keyword_filter,omitempty"`
	Rules          []SMSForwardingRule `json:"rules,omitempty"`
}

// SMSForwardingFailure records a failed SMS forwarding attempt.
type SMSForwardingFailure struct {
	Sender    string `json:"sender"`
	Timestamp int64  `json:"timestamp"`
	Error     string `json:"error"`
}

// RawSmsToolItem matches the JSON output from `sms_tool -j recv`.
type RawSmsToolItem struct {
	Index     interface{} `json:"index"`
	Sender    string      `json:"sender"`
	Timestamp string      `json:"timestamp"`
	Text      string      `json:"text"`
	Content   string      `json:"content"`
	Part      int         `json:"part,omitempty"`
	Total     int         `json:"total,omitempty"`
	Reference int         `json:"reference,omitempty"`
}

// SMSForwarder coordinates periodic SMS checking and rule-based dispatching.
type SMSForwarder struct {
	engine       *atengine.Engine
	configMgr    *config.Manager
	configPath   string
	failuresPath string
	seenPath     string
	smsToolPath  string
	atDevice     string
	interval     time.Duration
	httpClient   *http.Client

	seenMap map[string]bool
	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
}

// NewSMSForwarder initializes a background SMSForwarder.
func NewSMSForwarder(engine *atengine.Engine, cfgMgr *config.Manager) *SMSForwarder {
	cfgPath := os.Getenv("SMS_FORWARD_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = DefaultSMSForwardConfig
	}
	failPath := os.Getenv("SMS_FORWARD_FAILURES_PATH")
	if failPath == "" {
		failPath = DefaultSMSForwardFailures
	}
	seenPath := os.Getenv("SMS_FORWARD_SEEN_PATH")
	if seenPath == "" {
		seenPath = DefaultSMSForwardSeen
	}
	toolPath := os.Getenv("SMS_TOOL_PATH")
	if toolPath == "" {
		toolPath = DefaultSMSToolPath
	}
	atDev := os.Getenv("SMS_AT_DEVICE")
	if atDev == "" {
		atDev = DefaultSMSATDevice
	}

	return &SMSForwarder{
		engine:       engine,
		configMgr:    cfgMgr,
		configPath:   cfgPath,
		failuresPath: failPath,
		seenPath:     seenPath,
		smsToolPath:  toolPath,
		atDevice:     atDev,
		interval:     DefaultSMSPollInterval,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		seenMap:      make(map[string]bool),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the forwarder loop in background.
func (f *SMSForwarder) Start() {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	f.running = true
	f.loadSeenSet()
	f.mu.Unlock()

	go f.loop()
}

// Stop terminates the forwarder loop.
func (f *SMSForwarder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.running {
		return
	}
	f.running = false
	close(f.stopCh)
}

func (f *SMSForwarder) loop() {
	// Seed-on-first-run: when seen file does not exist, seed existing inbox without forwarding
	isFirstRun := len(f.seenMap) == 0 && !f.seenFileExists()
	if isFirstRun {
		f.runCycle(true)
	}

	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			f.runCycle(false)
		}
	}
}

func (f *SMSForwarder) seenFileExists() bool {
	_, err := os.Stat(f.seenPath)
	return err == nil
}

func (f *SMSForwarder) loadSeenSet() {
	data, err := os.ReadFile(f.seenPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			f.seenMap[l] = true
		}
	}
}

func (f *SMSForwarder) markSeen(fingerprint string) {
	f.seenMap[fingerprint] = true
	fHandle, err := os.OpenFile(f.seenPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		_, _ = fHandle.WriteString(fingerprint + "\n")
		_ = fHandle.Close()
	}
}

func (f *SMSForwarder) readConfig() SMSForwardingSettings {
	data, err := os.ReadFile(f.configPath)
	if err != nil {
		return SMSForwardingSettings{Enabled: false}
	}
	var cfg SMSForwardingSettings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return SMSForwardingSettings{Enabled: false}
	}
	return cfg
}

func (f *SMSForwarder) runCycle(seedOnly bool) {
	cfg := f.readConfig()
	if !cfg.Enabled && !seedOnly {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	messages, _, err := FetchInboxAndStorage(ctx, f.smsToolPath, f.atDevice, f.engine)
	if err != nil {
		return
	}

	for _, msg := range messages {
		fp := DJB2Fingerprint(msg.Storage, msg.Sender, msg.Timestamp, msg.Content)
		if f.seenMap[fp] {
			continue
		}

		if seedOnly {
			f.markSeen(fp)
			continue
		}

		// Loop guard: skip messages that look like our own relays
		if IsRelayMessage(msg.Content) {
			f.markSeen(fp)
			continue
		}

		// Keyword filter if configured
		if cfg.KeywordFilter != "" && !MatchesKeywordFilter(msg.Content, cfg.KeywordFilter) {
			f.markSeen(fp)
			continue
		}

		forwardSuccess := true
		var forwardErr string

		// 1. Phone Forwarding
		if cfg.TargetPhone != "" && ValidateTargetPhone(cfg.TargetPhone) {
			body := FormatForwardSMS(msg.Sender, msg.Content)
			if err := f.sendSMSWithRetry(ctx, cfg.TargetPhone, body); err != nil {
				forwardSuccess = false
				forwardErr = err.Error()
			}
		}

		// 2. Email Forwarding
		if cfg.EmailEnabled && cfg.EmailAddress != "" {
			if err := f.sendEmail(msg, cfg.EmailAddress); err != nil {
				forwardSuccess = false
				forwardErr = fmt.Sprintf("email: %v", err)
			}
		}

		// 3. Webhook Forwarding
		if cfg.WebhookEnabled && cfg.WebhookURL != "" {
			if err := f.sendWebhook(ctx, msg, cfg.WebhookURL); err != nil {
				forwardSuccess = false
				forwardErr = fmt.Sprintf("webhook: %v", err)
			}
		}

		// 4. Custom Rules
		if len(cfg.Rules) > 0 {
			matched := EvaluateForwardingRules(msg, cfg.Rules)
			for _, rule := range matched {
				f.executeRule(ctx, msg, rule)
			}
		}

		f.markSeen(fp)

		if !forwardSuccess && forwardErr != "" {
			f.recordFailure(msg.Sender, forwardErr)
		}
	}
}

func (f *SMSForwarder) executeRule(ctx context.Context, msg SMSMessage, rule SMSForwardingRule) {
	content := msg.Content
	if rule.CustomTemplate != "" {
		content = strings.ReplaceAll(rule.CustomTemplate, "{sender}", msg.Sender)
		content = strings.ReplaceAll(content, "{content}", msg.Content)
		content = strings.ReplaceAll(content, "{timestamp}", msg.Timestamp)
	}

	switch rule.TargetType {
	case "phone":
		_ = f.sendSMSWithRetry(ctx, rule.TargetEndpoint, content)
	case "email":
		_ = f.sendEmail(msg, rule.TargetEndpoint)
	case "webhook":
		_ = f.sendWebhook(ctx, msg, rule.TargetEndpoint)
	}
}

func (f *SMSForwarder) sendSMSWithRetry(ctx context.Context, targetPhone, body string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if f.engine != nil {
			regRes, err := f.engine.ExecContext(ctx, "AT+CREG?")
			if err == nil && !isRegistered(regRes.Raw) {
				cgregRes, err2 := f.engine.ExecContext(ctx, "AT+CGREG?")
				if err2 == nil && !isRegistered(cgregRes.Raw) {
					lastErr = fmt.Errorf("modem not registered on network")
					time.Sleep(3 * time.Second)
					continue
				}
			}
		}

		cleanPhone := strings.TrimPrefix(targetPhone, "+")

		if _, err := os.Stat(f.smsToolPath); err == nil {
			cmd := exec.CommandContext(ctx, f.smsToolPath, "-d", f.atDevice, "send", cleanPhone, body)
			out, err := cmd.CombinedOutput()
			if err == nil {
				return nil
			}
			lastErr = fmt.Errorf("sms_tool error: %s (%v)", strings.TrimSpace(string(out)), err)
		} else if f.engine != nil {
			_, _ = f.engine.ExecContext(ctx, "AT+CMGF=1")
			cmgsCmd := fmt.Sprintf("AT+CMGS=\"%s\"\r%s\x1A", targetPhone, body)
			res, err := f.engine.ExecContext(ctx, cmgsCmd)
			if err == nil && !strings.Contains(res.Raw, "ERROR") {
				return nil
			}
			lastErr = fmt.Errorf("AT CMGS error: %s (%v)", res.Raw, err)
		}

		time.Sleep(3 * time.Second)
	}

	return lastErr
}

func isRegistered(raw string) bool {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "+CREG:") || strings.Contains(l, "+CGREG:") {
			parts := strings.Split(l, ",")
			if len(parts) >= 2 {
				stat := strings.TrimSpace(parts[1])
				if stat == "1" || stat == "5" {
					return true
				}
			}
		}
	}
	return false
}

func (f *SMSForwarder) sendEmail(msg SMSMessage, toAddr string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	if host == "" || user == "" || pass == "" {
		return nil
	}
	if port == "" {
		port = "587"
	}
	if from == "" {
		from = user
	}

	auth := smtp.PlainAuth("", user, pass, host)
	subject := fmt.Sprintf("SMS from %s", msg.Sender)
	body := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\nSender: %s\nTimestamp: %s\nStorage: %s\n\n%s",
		from, toAddr, subject, msg.Sender, msg.Timestamp, msg.Storage, msg.Content)

	addr := fmt.Sprintf("%s:%s", host, port)
	return smtp.SendMail(addr, auth, from, []string{toAddr}, []byte(body))
}

func (f *SMSForwarder) sendWebhook(ctx context.Context, msg SMSMessage, endpoint string) error {
	payload := map[string]interface{}{
		"event":     "sms_received",
		"sender":    msg.Sender,
		"content":   msg.Content,
		"timestamp": msg.Timestamp,
		"storage":   msg.Storage,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QManager-SMSForwarder/1.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook responded with status %d", resp.StatusCode)
	}
	return nil
}

func (f *SMSForwarder) recordFailure(sender, errMsg string) {
	var failures []SMSForwardingFailure
	data, err := os.ReadFile(f.failuresPath)
	if err == nil {
		_ = json.Unmarshal(data, &failures)
	}

	record := SMSForwardingFailure{
		Sender:    sender,
		Timestamp: time.Now().Unix(),
		Error:     errMsg,
	}

	failures = append([]SMSForwardingFailure{record}, failures...)
	if len(failures) > MaxSMSForwardFailures {
		failures = failures[:MaxSMSForwardFailures]
	}

	outBytes, err := json.MarshalIndent(failures, "", "  ")
	if err == nil {
		_ = os.WriteFile(f.failuresPath, outBytes, 0644)
	}
	log.Printf("[SMSForwarder] Failure recorded for %s: %s", sender, errMsg)
}

// FetchInboxAndStorage reads inbox messages and storage statistics across ME and SM storage pools.
func FetchInboxAndStorage(ctx context.Context, smsToolPath, atDevice string, engine *atengine.Engine) ([]SMSMessage, SMSStorage, error) {
	if _, err := os.Stat(smsToolPath); err == nil {
		cmdInit := exec.CommandContext(ctx, smsToolPath, "-d", atDevice, "at", `AT+CPMS="ME","ME","ME"`)
		_ = cmdInit.Run()

		var rawME, rawSM []RawSmsToolItem
		cmdME := exec.CommandContext(ctx, smsToolPath, "-d", atDevice, "-s", "ME", "recv", "-j")
		if out, err := cmdME.Output(); err == nil {
			_ = json.Unmarshal(out, &rawME)
		}

		cmdSM := exec.CommandContext(ctx, smsToolPath, "-d", atDevice, "-s", "SM", "recv", "-j")
		if out, err := cmdSM.Output(); err == nil {
			_ = json.Unmarshal(out, &rawSM)
		}

		meMsgs := ConvertRawSmsItems(rawME, "ME")
		smMsgs := ConvertRawSmsItems(rawSM, "SM")

		merged := append(meMsgs, smMsgs...)
		SortSMSMessages(merged)

		meStat := ReadSmsToolStatus(ctx, smsToolPath, atDevice, "ME")
		smStat := ReadSmsToolStatus(ctx, smsToolPath, atDevice, "SM")

		return merged, SMSStorage{
			Used:  meStat.Used + smStat.Used,
			Total: meStat.Total + smStat.Total,
			ME:    &meStat,
			SM:    &smStat,
		}, nil
	}

	if engine != nil {
		_, _ = engine.ExecContext(ctx, `AT+CPMS="ME","ME","ME"`)
		resCPMS, _ := engine.ExecContext(ctx, "AT+CPMS?")
		meStat, smStat := ParseCPMSStorage(resCPMS.Raw)

		var allMsgs []SMSMessage
		for _, st := range []string{"ME", "SM"} {
			_, _ = engine.ExecContext(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, st, st, st))
			_, _ = engine.ExecContext(ctx, "AT+CMGF=1")
			res, err := engine.ExecContext(ctx, `AT+CMGL="ALL"`)
			if err == nil {
				msgs := ParseCMGLText(res.Raw, st)
				allMsgs = append(allMsgs, msgs...)
			}
		}

		SortSMSMessages(allMsgs)
		return allMsgs, SMSStorage{
			Used:  meStat.Used + smStat.Used,
			Total: meStat.Total + smStat.Total,
			ME:    &meStat,
			SM:    &smStat,
		}, nil
	}

	return []SMSMessage{}, SMSStorage{}, fmt.Errorf("no SMS backend available")
}

// ReadSmsToolStatus reads `sms_tool -s <storage> status`.
func ReadSmsToolStatus(ctx context.Context, toolPath, atDevice, storage string) MemoryStorage {
	cmd := exec.CommandContext(ctx, toolPath, "-d", atDevice, "-s", storage, "status")
	out, err := cmd.Output()
	if err != nil {
		return MemoryStorage{Used: 0, Total: 0}
	}
	return ParseSmsToolStatusOutput(string(out))
}

// ParseSmsToolStatusOutput parses `Storage type: ME, used: 0, total: 255`.
func ParseSmsToolStatusOutput(out string) MemoryStorage {
	stat := MemoryStorage{}
	parts := strings.Split(out, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "used:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(p, "used:"))
			stat.Used, _ = strconv.Atoi(valStr)
		} else if strings.HasPrefix(p, "total:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(p, "total:"))
			stat.Total, _ = strconv.Atoi(valStr)
		}
	}
	return stat
}

// ParseCPMSStorage parses AT+CPMS? response with storage triplets.
func ParseCPMSStorage(raw string) (MemoryStorage, MemoryStorage) {
	me := MemoryStorage{}
	sm := MemoryStorage{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CPMS:") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "+CPMS:"), ",")
		for i := 0; i+2 < len(parts); i += 3 {
			stName := strings.Trim(strings.TrimSpace(parts[i]), `"`)
			used, _ := strconv.Atoi(strings.TrimSpace(parts[i+1]))
			total, _ := strconv.Atoi(strings.TrimSpace(parts[i+2]))
			if stName == "ME" && me.Total == 0 {
				me = MemoryStorage{Used: used, Total: total}
			} else if stName == "SM" && sm.Total == 0 {
				sm = MemoryStorage{Used: used, Total: total}
			}
		}
	}
	return me, sm
}

// ConvertRawSmsItems merges multi-part SMS items from sms_tool output.
func ConvertRawSmsItems(rawItems []RawSmsToolItem, storage string) []SMSMessage {
	type groupKey struct {
		Sender    string
		Reference int
		Storage   string
	}

	multipartGroups := make(map[groupKey][]RawSmsToolItem)
	var singles []RawSmsToolItem

	for _, item := range rawItems {
		if item.Reference > 0 && item.Total > 1 {
			k := groupKey{Sender: item.Sender, Reference: item.Reference, Storage: storage}
			multipartGroups[k] = append(multipartGroups[k], item)
		} else {
			singles = append(singles, item)
		}
	}

	var result []SMSMessage

	for _, item := range singles {
		idxs := extractIndexes(item.Index)
		content := item.Content
		if content == "" {
			content = item.Text
		}
		result = append(result, SMSMessage{
			Indexes:   idxs,
			Sender:    item.Sender,
			Content:   content,
			Timestamp: item.Timestamp,
			Storage:   storage,
		})
	}

	for _, items := range multipartGroups {
		sort.Slice(items, func(i, j int) bool {
			return items[i].Part < items[j].Part
		})
		var combinedIndexes []int
		var textParts []string
		ts := ""
		sender := ""
		for _, part := range items {
			combinedIndexes = append(combinedIndexes, extractIndexes(part.Index)...)
			c := part.Content
			if c == "" {
				c = part.Text
			}
			textParts = append(textParts, c)
			if ts == "" {
				ts = part.Timestamp
			}
			if sender == "" {
				sender = part.Sender
			}
		}
		result = append(result, SMSMessage{
			Indexes:   combinedIndexes,
			Sender:    sender,
			Content:   strings.Join(textParts, ""),
			Timestamp: ts,
			Storage:   storage,
		})
	}

	return result
}

func extractIndexes(raw interface{}) []int {
	if raw == nil {
		return []int{}
	}
	switch v := raw.(type) {
	case float64:
		return []int{int(v)}
	case int:
		return []int{v}
	case []interface{}:
		var out []int
		for _, item := range v {
			if f, ok := item.(float64); ok {
				out = append(out, int(f))
			} else if i, ok := item.(int); ok {
				out = append(out, i)
			}
		}
		return out
	}
	return []int{}
}

// ParseCMGLText parses AT+CMGL="ALL" text-mode output.
func ParseCMGLText(raw string, storage string) []SMSMessage {
	var list []SMSMessage
	lines := strings.Split(raw, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "+CMGL:") {
			continue
		}

		header := strings.TrimPrefix(line, "+CMGL:")
		parts := strings.Split(header, ",")
		if len(parts) < 3 {
			continue
		}

		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		sender := strings.Trim(strings.TrimSpace(parts[2]), `"`)
		timestamp := ""
		if len(parts) >= 5 {
			timestamp = strings.Trim(strings.TrimSpace(parts[4]), `"`)
		}

		content := ""
		if i+1 < len(lines) {
			content = strings.TrimSpace(lines[i+1])
			i++
		}

		list = append(list, SMSMessage{
			Indexes:   []int{idx},
			Sender:    sender,
			Content:   content,
			Timestamp: timestamp,
			Storage:   storage,
		})
	}

	return list
}

// SortSMSMessages sorts SMS messages newest-first.
func SortSMSMessages(msgs []SMSMessage) {
	sort.Slice(msgs, func(i, j int) bool {
		tI := parseTimestampKey(msgs[i].Timestamp)
		tJ := parseTimestampKey(msgs[j].Timestamp)
		return tI > tJ
	})
}

func parseTimestampKey(ts string) string {
	ts = strings.TrimSpace(ts)
	if len(ts) == 17 && ts[2] == '/' && ts[5] == '/' && ts[8] == ' ' {
		mm := ts[0:2]
		dd := ts[3:5]
		yy := ts[6:8]
		rest := strings.ReplaceAll(ts[9:], ":", "")
		return yy + mm + dd + rest
	}
	return ts
}

// NormalizePhoneNumber normalizes phone number according to country code prefix.
func NormalizePhoneNumber(phone, defaultCountryCode string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	var sb strings.Builder
	for i, r := range phone {
		if r == '+' && i == 0 {
			sb.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	clean := sb.String()
	if clean == "" {
		return ""
	}

	if strings.HasPrefix(clean, "+") {
		return clean
	}

	if strings.HasPrefix(clean, "00") {
		return "+" + strings.TrimPrefix(clean, "00")
	}

	if strings.HasPrefix(clean, "0") && defaultCountryCode != "" {
		return "+" + defaultCountryCode + strings.TrimPrefix(clean, "0")
	}

	return clean
}

// ValidateTargetPhone validates phone number (E.164-ish, 7-15 digits, non-zero first digit).
func ValidateTargetPhone(phone string) bool {
	p := strings.TrimPrefix(strings.TrimSpace(phone), "+")
	if len(p) < 7 || len(p) > 15 {
		return false
	}
	if p[0] == '0' {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FormatForwardSMS formats SMS forward text: "From <sender>: <content>"
func FormatForwardSMS(sender, content string) string {
	return fmt.Sprintf("From %s: %s", sender, content)
}

// IsRelayMessage checks if content looks like a forwarded relay to prevent loops.
func IsRelayMessage(content string) bool {
	if !strings.HasPrefix(content, "From ") {
		return false
	}
	colonIdx := strings.Index(content, ": ")
	if colonIdx <= 5 {
		return false
	}
	senderToken := content[5:colonIdx]
	senderClean := strings.TrimPrefix(senderToken, "+")
	if len(senderClean) == 0 {
		return false
	}
	for _, r := range senderClean {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// DJB2Fingerprint computes the 32-bit djb2 hash over "storage|sender|timestamp|content".
func DJB2Fingerprint(storage, sender, timestamp, content string) string {
	s := fmt.Sprintf("%s|%s|%s|%s", storage, sender, timestamp, content)
	var h uint32 = 5381
	for i := 0; i < len(s); i++ {
		h = ((h << 5) + h) + uint32(s[i])
	}
	return fmt.Sprintf("%d", h)
}

// MatchesKeywordFilter checks if SMS content matches configured keywords.
func MatchesKeywordFilter(content, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}

	keywords := strings.Split(filter, ",")
	contentLower := strings.ToLower(content)

	for _, kw := range keywords {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw == "" {
			continue
		}
		if strings.HasPrefix(kw, "regex:") {
			rePattern := strings.TrimPrefix(kw, "regex:")
			if re, err := regexp.Compile("(?i)" + rePattern); err == nil && re.MatchString(content) {
				return true
			}
		} else {
			if strings.Contains(contentLower, kw) {
				return true
			}
		}
	}

	return false
}

// EvaluateForwardingRules checks message against custom rules and returns matched rules.
func EvaluateForwardingRules(msg SMSMessage, rules []SMSForwardingRule) []SMSForwardingRule {
	var matched []SMSForwardingRule
	for _, r := range rules {
		if !r.Enabled {
			continue
		}

		senderMatch := true
		if r.MatchSender != "" {
			senderMatch = strings.EqualFold(msg.Sender, r.MatchSender) ||
				strings.Contains(msg.Sender, r.MatchSender)
			if !senderMatch {
				if re, err := regexp.Compile("(?i)" + r.MatchSender); err == nil && re.MatchString(msg.Sender) {
					senderMatch = true
				}
			}
		}

		keywordMatch := true
		if r.MatchKeyword != "" {
			keywordMatch = MatchesKeywordFilter(msg.Content, r.MatchKeyword)
		}

		if senderMatch && keywordMatch {
			matched = append(matched, r)
		}
	}
	return matched
}
