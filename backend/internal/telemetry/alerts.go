package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"sync"
	"time"

	"qmanager/internal/atengine"
)

// AlertPayload holds notification content.
type AlertPayload struct {
	Level     string    `json:"level"`     // "INFO", "WARNING", "CRITICAL"
	Title     string    `json:"title"`     // e.g. "Signal Degraded"
	Message   string    `json:"message"`   // e.g. "RSRP dropped below -115 dBm"
	Timestamp time.Time `json:"timestamp"`
}

// EmailConfig holds SMTP credentials.
type EmailConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// SMSAlertConfig holds target phone for SMS notifications.
type SMSAlertConfig struct {
	Enabled     bool   `json:"enabled"`
	PhoneNumber string `json:"phone_number"`
}

// DiscordConfig holds Discord webhook notifications config.
type DiscordConfig struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Username   string `json:"username,omitempty"`
	Token      string `json:"token,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
}

// WebhookConfig holds generic webhook notification settings.
type WebhookConfig struct {
	Enabled bool              `json:"enabled"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// AlertDispatcher handles routing alerts to enabled channels (SMS, Email, Discord, Webhook).
type AlertDispatcher struct {
	engine     *atengine.Engine
	emailCfg   EmailConfig
	smsCfg     SMSAlertConfig
	discordCfg DiscordConfig
	webhookCfg WebhookConfig
	httpClient *http.Client
	mu         sync.RWMutex
	history    []AlertPayload
	maxEvents  int
}

// NewAlertDispatcher creates a new alert manager.
func NewAlertDispatcher(eng *atengine.Engine) *AlertDispatcher {
	return &AlertDispatcher{
		engine: eng,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		history:   make([]AlertPayload, 0, 50),
		maxEvents: 50,
	}
}

// SetEmailConfig updates email dispatch settings.
func (a *AlertDispatcher) SetEmailConfig(cfg EmailConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emailCfg = cfg
}

// SetSMSConfig updates SMS dispatch settings.
func (a *AlertDispatcher) SetSMSConfig(cfg SMSAlertConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.smsCfg = cfg
}

// SetDiscordConfig updates Discord webhook settings.
func (a *AlertDispatcher) SetDiscordConfig(cfg DiscordConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.discordCfg = cfg
}

// SetWebhookConfig updates generic webhook settings.
func (a *AlertDispatcher) SetWebhookConfig(cfg WebhookConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.webhookCfg = cfg
}

// Dispatch sends alert to all enabled destinations asynchronously.
func (a *AlertDispatcher) Dispatch(ctx context.Context, alert AlertPayload) {
	a.mu.Lock()
	if len(a.history) >= a.maxEvents {
		a.history = a.history[1:]
	}
	a.history = append(a.history, alert)
	emailCfg := a.emailCfg
	smsCfg := a.smsCfg
	discordCfg := a.discordCfg
	webhookCfg := a.webhookCfg
	a.mu.Unlock()

	// 1. Send Email
	if emailCfg.Enabled && emailCfg.Host != "" && emailCfg.To != "" {
		go a.sendEmail(emailCfg, alert)
	}

	// 2. Send SMS via AT Engine
	if smsCfg.Enabled && smsCfg.PhoneNumber != "" && a.engine != nil {
		go a.sendSMS(ctx, smsCfg.PhoneNumber, fmt.Sprintf("[%s] %s: %s", alert.Level, alert.Title, alert.Message))
	}

	// 3. Send Discord Webhook
	if discordCfg.Enabled && discordCfg.WebhookURL != "" {
		go a.sendDiscord(discordCfg, alert)
	}

	// 4. Send Generic Webhook
	if webhookCfg.Enabled && webhookCfg.URL != "" {
		go a.sendWebhook(webhookCfg, alert)
	}
}

func (a *AlertDispatcher) sendDiscord(cfg DiscordConfig, alert AlertPayload) {
	// Pick color based on alert level
	color := 3447003 // Blue / INFO default
	switch alert.Level {
	case "WARNING":
		color = 16776960 // Yellow
	case "CRITICAL", "ERROR":
		color = 16711680 // Red
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("[%s] %s", alert.Level, alert.Title),
		"description": alert.Message,
		"color":       color,
		"timestamp":   alert.Timestamp.UTC().Format(time.RFC3339),
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}
	if cfg.Username != "" {
		payload["username"] = cfg.Username
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequest(http.MethodPost, cfg.WebhookURL, bytes.NewBuffer(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bot "+cfg.Token)
	}

	resp, err := a.httpClient.Do(req)
	if err == nil && resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func (a *AlertDispatcher) sendWebhook(cfg WebhookConfig, alert AlertPayload) {
	data, err := json.Marshal(alert)
	if err != nil {
		return
	}

	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewBuffer(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err == nil && resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func (a *AlertDispatcher) sendEmail(cfg EmailConfig, alert AlertPayload) {
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: [%s] QManager Alert: %s\r\n\r\n%s\r\nTimestamp: %s\r\n",
		cfg.From, cfg.To, alert.Level, alert.Title, alert.Message, alert.Timestamp.Format(time.RFC1123))

	_ = smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, []byte(msg))
}

func (a *AlertDispatcher) sendSMS(ctx context.Context, phone, message string) {
	// SMS text mode sending via AT+CMGS
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, _ = a.engine.ExecContext(reqCtx, "AT+CMGF=1")
	// Quectel CMGS syntax: AT+CMGS="phone"<CR>message<Ctrl+Z>
	cmgsCmd := fmt.Sprintf("AT+CMGS=\"%s\"\r%s\x1A", phone, message)
	_, _ = a.engine.ExecContext(reqCtx, cmgsCmd)
}

// GetHistory returns recent alerts.
func (a *AlertDispatcher) GetHistory() []AlertPayload {
	a.mu.RLock()
	defer a.mu.RUnlock()

	copied := make([]AlertPayload, len(a.history))
	copy(copied, a.history)
	return copied
}
