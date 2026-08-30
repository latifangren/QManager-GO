package telemetry

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
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

// AlertDispatcher handles routing alerts to enabled channels (SMS, Email).
type AlertDispatcher struct {
	engine    *atengine.Engine
	emailCfg  EmailConfig
	smsCfg    SMSAlertConfig
	mu        sync.RWMutex
	history   []AlertPayload
	maxEvents int
}

// NewAlertDispatcher creates a new alert manager.
func NewAlertDispatcher(eng *atengine.Engine) *AlertDispatcher {
	return &AlertDispatcher{
		engine:    eng,
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

// Dispatch sends alert to all enabled destinations asynchronously.
func (a *AlertDispatcher) Dispatch(ctx context.Context, alert AlertPayload) {
	a.mu.Lock()
	if len(a.history) >= a.maxEvents {
		a.history = a.history[1:]
	}
	a.history = append(a.history, alert)
	emailCfg := a.emailCfg
	smsCfg := a.smsCfg
	a.mu.Unlock()

	// 1. Send Email
	if emailCfg.Enabled && emailCfg.Host != "" && emailCfg.To != "" {
		go a.sendEmail(emailCfg, alert)
	}

	// 2. Send SMS via AT Engine
	if smsCfg.Enabled && smsCfg.PhoneNumber != "" && a.engine != nil {
		go a.sendSMS(ctx, smsCfg.PhoneNumber, fmt.Sprintf("[%s] %s: %s", alert.Level, alert.Title, alert.Message))
	}
}

func (a *AlertDispatcher) sendEmail(cfg EmailConfig, alert AlertPayload) {
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: [%s] QManager Alert: %s\r\n\r\n%s\r\nTimestamp: %s\r\n",
		cfg.From, cfg.To, alert.Level, alert.Title, alert.Message, alert.Timestamp.Format(time.RFC1123))

	_ = smtp.SendMail(addr, auth, cfg.From, strings.Split(cfg.To, ","), []byte(msg))
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
