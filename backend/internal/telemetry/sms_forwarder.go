package telemetry

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
)

const (
	DefaultSMSForwardConfig   = "/etc/qmanager/sms_forwarding.json"
	DefaultSMSForwardFailures = "/tmp/qmanager_sms_forward_failures.json"
	DefaultSMSForwardSeen     = "/tmp/qmanager_sms_forward_seen"
	DefaultSMSForwardReload   = "/tmp/qmanager_sms_forward_reload"
	DefaultSMSForwardUnitName = "qmanager-sms-forward.service"
	DefaultSMSPollInterval    = 15 * time.Second
	MaxSMSForwardFailures     = 20
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
	MatchSender    string `json:"match_sender,omitempty"`
	MatchKeyword   string `json:"match_keyword,omitempty"`
	TargetType     string `json:"target_type"` // "phone", "email", "webhook"
	TargetEndpoint string `json:"target_endpoint"`
	ConditionField string `json:"condition_field,omitempty"` // "sender", "content", "any"
	ConditionOp    string `json:"condition_op,omitempty"`    // "contains", "equals", "starts_with", "regex"
	ConditionValue string `json:"condition_value,omitempty"`
	CustomTemplate string `json:"custom_template,omitempty"`
}

// SMSForwardingSettings matches settings persisted in JSON.
type SMSForwardingSettings struct {
	Enabled        bool                `json:"enabled"`
	TargetPhone    string              `json:"target_phone,omitempty"`
	EmailEnabled   bool                `json:"email_enabled,omitempty"`
	EmailAddress   string              `json:"email_address,omitempty"`
	SMTPServer     string              `json:"smtp_server,omitempty"`
	SMTPPort       int                 `json:"smtp_port,omitempty"`
	SMTPUser       string              `json:"smtp_user,omitempty"`
	SMTPPass       string              `json:"smtp_pass,omitempty"`
	WebhookEnabled bool                `json:"webhook_enabled,omitempty"`
	WebhookURL     string              `json:"webhook_url,omitempty"`
	KeywordFilter  string              `json:"keyword_filter,omitempty"`
	Rules          []SMSForwardingRule `json:"rules,omitempty"`
}

// SMSForwardingConfig is alias for SMSForwardingSettings.
type SMSForwardingConfig = SMSForwardingSettings

// SMSForwardingFailure records a failed SMS forwarding attempt.
type SMSForwardingFailure struct {
	Sender    string `json:"sender"`
	Timestamp int64  `json:"timestamp"`
	Error     string `json:"error"`
}

// RawSmsToolItem matches legacy JSON structure for backwards compatibility.
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

	return &SMSForwarder{
		engine:       engine,
		configMgr:    cfgMgr,
		configPath:   cfgPath,
		failuresPath: failPath,
		seenPath:     seenPath,
		interval:     DefaultSMSPollInterval,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		seenMap:      make(map[string]bool),
		stopCh:       make(chan struct{}),
	}
}

// Start begins periodic background polling for incoming SMS messages.
func (f *SMSForwarder) Start() {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	f.running = true
	f.stopCh = make(chan struct{})
	f.mu.Unlock()

	f.loadSeen()

	// Initial seed scan to prevent blasting old inbox SMS as newly forwarded on boot
	f.pollOnce(true)

	go func() {
		ticker := time.NewTicker(f.interval)
		defer ticker.Stop()

		for {
			select {
			case <-f.stopCh:
				return
			case <-ticker.C:
				if _, err := os.Stat(DefaultSMSForwardReload); err == nil {
					_ = os.Remove(DefaultSMSForwardReload)
					f.loadSeen()
				}
				f.pollOnce(false)
			}
		}
	}()
}

// Stop terminates background SMS forwarder loop.
func (f *SMSForwarder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.running {
		return
	}
	close(f.stopCh)
	f.running = false
}

// runCycle runs one forwarding cycle (for testing).
func (f *SMSForwarder) runCycle(seedOnly bool) {
	f.pollOnce(seedOnly)
}

func (f *SMSForwarder) seenFileExists() bool {
	_, err := os.Stat(f.seenPath)
	return err == nil
}

// loadSeen loads seen fingerprint hashes from tmpfs to prevent duplicate relays.
func (f *SMSForwarder) loadSeen() {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := os.ReadFile(f.seenPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			f.seenMap[trimmed] = true
		}
	}
}

// markSeen appends fingerprint to seen tracking list.
func (f *SMSForwarder) markSeen(fingerprint string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.seenMap[fingerprint] = true
	dir := filepath.Dir(f.seenPath)
	_ = os.MkdirAll(dir, 0755)
	fEntry, err := os.OpenFile(f.seenPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = fEntry.WriteString(fingerprint + "\n")
		_ = fEntry.Close()
	}
}

// readConfig loads current forwarding configuration from file or memory.
func (f *SMSForwarder) readConfig() SMSForwardingSettings {
	var cfg SMSForwardingSettings
	data, err := os.ReadFile(f.configPath)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	return cfg
}

// pollOnce executes a single inbox check and forwards new messages.
func (f *SMSForwarder) pollOnce(seedOnly bool) {
	cfg := f.readConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	messages, _, err := FetchInboxAndStorage(ctx, "", "", f.engine)
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

		if !cfg.Enabled {
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

		// 4. Custom Conditional Rules
		matchedRules := EvaluateForwardingRules(msg, cfg.Rules)
		for _, rule := range matchedRules {
			f.executeRule(ctx, msg, rule)
		}

		if forwardSuccess {
			f.markSeen(fp)
		} else {
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
		if f.engine != nil {
			_, _ = f.engine.ExecContext(ctx, "AT+CMGF=1")
			cmgsCmd := fmt.Sprintf("AT+CMGS=\"%s\"\r%s\x1A", cleanPhone, body)
			res, err := f.engine.ExecContext(ctx, cmgsCmd)
			if err == nil && !strings.Contains(res.Raw, "ERROR") {
				return nil
			}
			lastErr = fmt.Errorf("AT engine error: %s (%v)", res.Raw, err)
		} else {
			lastErr = fmt.Errorf("no SMS engine transport available")
		}

		time.Sleep(2 * time.Second)
	}
	return lastErr
}

func isRegistered(raw string) bool {
	for _, stat := range []string{",1", ",5", ", 1", ", 5"} {
		if strings.Contains(raw, stat) {
			return true
		}
	}
	return false
}

func (f *SMSForwarder) sendEmail(msg SMSMessage, toAddr string) error {
	cfg := f.readConfig()
	if cfg.SMTPServer == "" || toAddr == "" {
		return fmt.Errorf("SMTP server or recipient not configured")
	}

	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", cfg.SMTPServer, port)

	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPServer)
	}

	subject := fmt.Sprintf("[SMS Forward] From %s", msg.Sender)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nReceived: %s\r\nFrom: %s\r\nStorage: %s\r\n\r\n%s",
		cfg.SMTPUser, toAddr, subject, msg.Timestamp, msg.Sender, msg.Storage, msg.Content)

	return smtp.SendMail(addr, auth, cfg.SMTPUser, []string{toAddr}, []byte(body))
}

func (f *SMSForwarder) sendWebhook(ctx context.Context, msg SMSMessage, url string) error {
	payload, err := json.Marshal(map[string]interface{}{
		"event":     "sms_received",
		"sender":    msg.Sender,
		"content":   msg.Content,
		"timestamp": msg.Timestamp,
		"storage":   msg.Storage,
		"indexes":   msg.Indexes,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
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
		return fmt.Errorf("webhook responded with HTTP %d", resp.StatusCode)
	}
	return nil
}

func (f *SMSForwarder) recordFailure(sender, errDetail string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var failures []SMSForwardingFailure
	data, err := os.ReadFile(f.failuresPath)
	if err == nil {
		_ = json.Unmarshal(data, &failures)
	}

	failures = append(failures, SMSForwardingFailure{
		Sender:    sender,
		Timestamp: time.Now().Unix(),
		Error:     errDetail,
	})

	if len(failures) > MaxSMSForwardFailures {
		failures = failures[len(failures)-MaxSMSForwardFailures:]
	}

	if payload, err := json.Marshal(failures); err == nil {
		_ = os.WriteFile(f.failuresPath, payload, 0644)
	}
}

// ParseSmsToolOutput parses legacy JSON format.
func ParseSmsToolOutput(out []byte) []RawSmsToolItem {
	var raw []RawSmsToolItem
	if err := json.Unmarshal(out, &raw); err == nil {
		return raw
	}
	var envelope struct {
		Msg []RawSmsToolItem `json:"msg"`
	}
	if err := json.Unmarshal(out, &envelope); err == nil {
		return envelope.Msg
	}
	if raw != nil {
		return raw
	}
	return nil
}

// ParseSmsToolStatusOutput parses `Storage type: ME, used: 0, total: 255`.
func ParseSmsToolStatusOutput(out string) MemoryStorage {
	stat := MemoryStorage{}
	out = strings.TrimSpace(out)
	if strings.Contains(out, "used:") {
		parts := strings.Split(out, "used:")
		if len(parts) >= 2 {
			valStr := strings.TrimSpace(parts[1])
			if strings.Contains(valStr, ",") {
				valStr = strings.TrimSpace(strings.Split(valStr, ",")[0])
			}
			stat.Used, _ = strconv.Atoi(valStr)
		}
	}
	if strings.Contains(out, "total:") {
		parts := strings.Split(out, "total:")
		if len(parts) >= 2 {
			valStr := strings.TrimSpace(parts[1])
			if strings.Contains(valStr, ",") {
				valStr = strings.TrimSpace(strings.Split(valStr, ",")[0])
			}
			stat.Total, _ = strconv.Atoi(valStr)
		}
	}
	return stat
}

func parseTimestampKey(ts string) string {
	ts = strings.TrimSpace(ts)
	if len(ts) == 17 && ts[2] == '/' && ts[5] == '/' && ts[8] == ' ' {
		// "09/12/26 10:15:30" -> "260912101530"
		return ts[6:8] + ts[0:2] + ts[3:5] + strings.ReplaceAll(ts[9:], ":", "")
	}
	return ts
}

// FetchInboxAndStorage reads inbox messages and storage statistics across ME and SM storage pools.
func FetchInboxAndStorage(ctx context.Context, _, _ string, engine *atengine.Engine) ([]SMSMessage, SMSStorage, error) {
	if engine == nil {
		return []SMSMessage{}, SMSStorage{}, fmt.Errorf("no AT engine available")
	}

	// 1. Query storage capacity (AT+CPMS?)
	resCPMS, err := engine.ExecContext(ctx, "AT+CPMS?")
	if err != nil {
		log.Printf("[SMS] Failed to query CPMS storage: %v", err)
	}
	meStat, smStat := ParseCPMSStorage(resCPMS.Raw)

	// 2. Fetch messages for each storage pool
	var allMsgs []SMSMessage
	for _, st := range []string{"ME", "SM"} {
		stat := meStat
		if st == "SM" {
			stat = smStat
		}
		if stat.Total > 0 && stat.Used == 0 {
			continue
		}

		_, _ = engine.ExecContext(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, st, st, st))
		// Try PDU mode first (AT+CMGF=0)
		_, _ = engine.ExecContext(ctx, "AT+CMGF=0")
		resCMGL, err := engine.ExecContext(ctx, "AT+CMGL=4")
		if err == nil && strings.Contains(resCMGL.Raw, "+CMGL:") {
			pduItems := ParseCMGLPDU(resCMGL.Raw, st)
			if len(pduItems) > 0 {
				allMsgs = append(allMsgs, ReassembleMultipartSMS(pduItems)...)
				continue
			}
		}

		// Fallback: Text mode (AT+CMGF=1)
		_, _ = engine.ExecContext(ctx, "AT+CMGF=1")
		resCMGLText, errText := engine.ExecContext(ctx, `AT+CMGL="ALL"`)
		if errText == nil && strings.Contains(resCMGLText.Raw, "+CMGL:") {
			textMsgs := ParseCMGLText(resCMGLText.Raw, st)
			allMsgs = append(allMsgs, textMsgs...)
		}
	}

	SortSMSMessages(allMsgs)
	if allMsgs == nil {
		allMsgs = []SMSMessage{}
	}

	return allMsgs, SMSStorage{
		Used:  meStat.Used + smStat.Used,
		Total: meStat.Total + smStat.Total,
		ME:    &meStat,
		SM:    &smStat,
	}, nil
}

// ParseCPMSStorage parses AT+CPMS? response with storage triplets.
func ParseCPMSStorage(raw string) (MemoryStorage, MemoryStorage) {
	me := MemoryStorage{Total: 127}
	sm := MemoryStorage{Total: 30}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CPMS:") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "+CPMS:"), ",")
		if len(parts) >= 2 {
			// Check if first part is a number (unnamed format e.g. "+CPMS: 1,255,1,255,0,255")
			if u, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
				tot, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				me = MemoryStorage{Used: u, Total: tot}
				if len(parts) >= 4 {
					u2, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
					tot2, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
					sm = MemoryStorage{Used: u2, Total: tot2}
				}
				return me, sm
			}
		}
		for i := 0; i+2 < len(parts); i += 3 {
			stName := strings.Trim(strings.TrimSpace(parts[i]), `"`)
			used, _ := strconv.Atoi(strings.TrimSpace(parts[i+1]))
			total, _ := strconv.Atoi(strings.TrimSpace(parts[i+2]))
			if stName == "ME" {
				me = MemoryStorage{Used: used, Total: total}
			} else if stName == "SM" {
				sm = MemoryStorage{Used: used, Total: total}
			}
		}
	}
	return me, sm
}

// ConvertRawSmsItems merges multi-part SMS items.
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

	for _, group := range multipartGroups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Part < group[j].Part
		})

		var combinedIndexes []int
		var contentBuilder strings.Builder
		sender := ""
		timestamp := ""

		for _, part := range group {
			combinedIndexes = append(combinedIndexes, extractIndexes(part.Index)...)
			c := part.Content
			if c == "" {
				c = part.Text
			}
			contentBuilder.WriteString(c)
			if sender == "" {
				sender = part.Sender
			}
			if timestamp == "" {
				timestamp = part.Timestamp
			}
		}

		result = append(result, SMSMessage{
			Indexes:   combinedIndexes,
			Sender:    sender,
			Content:   contentBuilder.String(),
			Timestamp: timestamp,
			Storage:   storage,
		})
	}

	return result
}

func extractIndexes(val interface{}) []int {
	switch v := val.(type) {
	case int:
		return []int{v}
	case float64:
		return []int{int(v)}
	case string:
		// parse range or list
		if strings.Contains(v, "-") {
			parts := strings.Split(v, "-")
			if len(parts) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err1 == nil && err2 == nil && start <= end {
					var out []int
					for i := start; i <= end; i++ {
						out = append(out, i)
					}
					return out
				}
			}
		}
		if strings.Contains(v, ",") {
			parts := strings.Split(v, ",")
			var out []int
			for _, p := range parts {
				out = append(out, extractIndexes(strings.TrimSpace(p))...)
			}
			return out
		}
		if i, err := strconv.Atoi(v); err == nil {
			return []int{i}
		}
	case []interface{}:
		var out []int
		for _, el := range v {
			out = append(out, extractIndexes(el)...)
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
		sender := decodeUCS2HexString(strings.Trim(strings.TrimSpace(parts[2]), `"`))
		timestamp := ""
		if len(parts) >= 5 {
			timestamp = strings.Trim(strings.TrimSpace(parts[4]), `"`)
		}

		content := ""
		if i+1 < len(lines) {
			content = decodeUCS2HexString(strings.TrimSpace(lines[i+1]))
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

// decodeUCS2HexString decodes hex-encoded UTF-16BE / UCS2 strings.
func decodeUCS2HexString(hexStr string) string {
	hexStr = strings.TrimSpace(hexStr)
	if len(hexStr) >= 4 && len(hexStr)%4 == 0 {
		b, err := hex.DecodeString(hexStr)
		if err == nil && len(b)%2 == 0 {
			u16 := make([]uint16, len(b)/2)
			for i := 0; i < len(u16); i++ {
				u16[i] = binary.BigEndian.Uint16(b[i*2 : i*2+2])
			}
			return string(utf16.Decode(u16))
		}
	}
	return hexStr
}

// SortSMSMessages orders SMS messages by timestamp descending (newest first).
func SortSMSMessages(msgs []SMSMessage) {
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Timestamp > msgs[j].Timestamp
	})
}

// DJB2Fingerprint produces a hash of storage+sender+timestamp+content for deduplication.
func DJB2Fingerprint(storage, sender, timestamp, content string) string {
	combined := fmt.Sprintf("%s|%s|%s|%s", storage, sender, timestamp, content)
	var hash uint64 = 5381
	for i := 0; i < len(combined); i++ {
		hash = ((hash << 5) + hash) + uint64(combined[i])
	}
	return fmt.Sprintf("%x", hash)
}

// IsRelayMessage checks if content originates from our own forwarder prefix.
func IsRelayMessage(content string) bool {
	return strings.HasPrefix(content, "From +") || strings.HasPrefix(content, "From 0") ||
		strings.HasPrefix(content, "From 1") || strings.HasPrefix(content, "From 2") ||
		strings.HasPrefix(content, "From 3") || strings.HasPrefix(content, "From 4") ||
		strings.HasPrefix(content, "From 5") || strings.HasPrefix(content, "From 6") ||
		strings.HasPrefix(content, "From 7") || strings.HasPrefix(content, "From 8") ||
		strings.HasPrefix(content, "From 9") || strings.HasPrefix(content, "[Fwd:")
}

// MatchesKeywordFilter checks if message content matches case-insensitive keyword or regex.
func MatchesKeywordFilter(content, filter string) bool {
	if filter == "" {
		return true
	}
	if strings.HasPrefix(filter, "regex:") {
		pattern := strings.TrimPrefix(filter, "regex:")
		if re, err := regexp.Compile("(?i)" + pattern); err == nil {
			return re.MatchString(content)
		}
	}
	for _, kw := range strings.Split(filter, ",") {
		kw = strings.TrimSpace(kw)
		if kw != "" && strings.Contains(strings.ToLower(content), strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// EvaluateForwardingRules checks message against rule list and returns matching rules.
func EvaluateForwardingRules(msg SMSMessage, rules []SMSForwardingRule) []SMSForwardingRule {
	var matched []SMSForwardingRule
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.MatchSender != "" {
			if re, err := regexp.Compile("(?i)" + rule.MatchSender); err == nil {
				if !re.MatchString(msg.Sender) {
					continue
				}
			} else if !strings.EqualFold(msg.Sender, rule.MatchSender) {
				continue
			}
		}
		if rule.MatchKeyword != "" {
			if !MatchesKeywordFilter(msg.Content, rule.MatchKeyword) {
				continue
			}
		}
		matched = append(matched, rule)
	}
	return matched
}

// MatchesRule tests if an SMS message matches a conditional rule.
func MatchesRule(msg SMSMessage, rule SMSForwardingRule) bool {
	if !rule.Enabled {
		return false
	}
	if rule.MatchSender != "" {
		if re, err := regexp.Compile("(?i)" + rule.MatchSender); err == nil {
			if !re.MatchString(msg.Sender) {
				return false
			}
		} else if !strings.EqualFold(msg.Sender, rule.MatchSender) {
			return false
		}
	}
	if rule.MatchKeyword != "" {
		return MatchesKeywordFilter(msg.Content, rule.MatchKeyword)
	}
	return true
}

// FormatForwardSMS formats SMS payload with standard forward header.
func FormatForwardSMS(sender, content string) string {
	return fmt.Sprintf("From %s: %s", sender, content)
}

// ValidateTargetPhone ensures phone number is valid international/national format.
func ValidateTargetPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "0") {
		return false
	}
	cleaned := strings.TrimPrefix(phone, "+")
	if len(cleaned) < 7 || len(cleaned) > 15 {
		return false
	}
	for _, c := range cleaned {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// NormalizePhoneNumber strips spaces, hyphens, and formats standard dial numbers.
func NormalizePhoneNumber(phone, defaultPrefix string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	var sb strings.Builder
	for i, c := range phone {
		if c == '+' && i == 0 {
			sb.WriteRune(c)
		} else if c >= '0' && c <= '9' {
			sb.WriteRune(c)
		}
	}
	res := sb.String()
	if strings.HasPrefix(res, "00") {
		res = "+" + strings.TrimPrefix(res, "00")
	} else if defaultPrefix != "" && !strings.HasPrefix(res, "+") && strings.HasPrefix(res, "0") {
		res = "+" + defaultPrefix + strings.TrimPrefix(res, "0")
	} else if !strings.HasPrefix(res, "+") && len(res) > 0 {
		res = "+" + res
	}
	return res
}
