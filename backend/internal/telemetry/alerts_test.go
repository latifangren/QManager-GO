package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qmanager/internal/atengine"
)

func TestAlertDispatcher_ConfigurationAndDispatch(t *testing.T) {
	mock := atengine.NewMockTransport()
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CMGS=\"+1234567890\"\r[CRITICAL] High Temp: Modem temp 85C\x1A", "OK")

	eng := atengine.NewEngine(mock)
	dispatcher := NewAlertDispatcher(eng)

	// Mock Discord server
	discordReceived := make(chan bool, 1)
	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordReceived <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer discordServer.Close()

	// Mock Generic Webhook server
	webhookReceived := make(chan bool, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookReceived <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	// Configure SMS, Email, Discord, Webhook
	dispatcher.SetSMSConfig(SMSAlertConfig{
		Enabled:     true,
		PhoneNumber: "+1234567890",
	})
	dispatcher.SetEmailConfig(EmailConfig{
		Enabled:  false, // Keep disabled so no external SMTP call is attempted
		Host:     "smtp.example.com",
		Port:     587,
		Username: "user",
		Password: "password",
	})
	dispatcher.SetDiscordConfig(DiscordConfig{
		Enabled:    true,
		WebhookURL: discordServer.URL,
		Username:   "QManagerBot",
	})
	dispatcher.SetWebhookConfig(WebhookConfig{
		Enabled: true,
		URL:     webhookServer.URL,
		Headers: map[string]string{"X-Test": "Alert"},
	})

	alert := AlertPayload{
		Level:     "CRITICAL",
		Title:     "High Temp",
		Message:   "Modem temp 85C",
		Timestamp: time.Now().UTC(),
	}

	ctx := context.Background()
	dispatcher.Dispatch(ctx, alert)

	history := dispatcher.GetHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 alert in history, got %d", len(history))
	}

	// Verify Discord and Webhook deliveries
	select {
	case <-discordReceived:
	case <-time.After(2 * time.Second):
		t.Errorf("timed out waiting for discord alert")
	}

	select {
	case <-webhookReceived:
	case <-time.After(2 * time.Second):
		t.Errorf("timed out waiting for webhook alert")
	}
}

func TestAlertDispatcher_SendEmailAndErrorLogging(t *testing.T) {
	dispatcher := NewAlertDispatcher(nil)

	// Send email to non-existent server (should not panic or crash daemon)
	cfg := EmailConfig{
		Enabled:  true,
		Host:     "127.0.0.1",
		Port:     65530,
		Username: "none",
		Password: "none",
		From:     "alerts@example.com",
		To:       "admin@example.com",
	}

	alert := AlertPayload{
		Level:     "WARNING",
		Title:     "Degraded",
		Message:   "Packet loss > 20%",
		Timestamp: time.Now().UTC(),
	}

	// Direct call to sendEmail should complete cleanly without panicking
	dispatcher.sendEmail(cfg, alert)

	// Direct call to sendDiscord with invalid URL
	dispatcher.sendDiscord(DiscordConfig{
		Enabled:    true,
		WebhookURL: "http://127.0.0.1:65530/invalid",
	}, alert)

	// Direct call to sendWebhook with invalid URL
	dispatcher.sendWebhook(WebhookConfig{
		Enabled: true,
		URL:     "http://127.0.0.1:65530/invalid",
	}, alert)
}
