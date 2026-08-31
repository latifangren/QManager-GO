package telemetry

import (
	"context"
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

	// Configure SMS and Email
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
	if history[0].Title != "High Temp" || history[0].Level != "CRITICAL" {
		t.Errorf("unexpected alert payload in history: %+v", history[0])
	}
}

func TestAlertDispatcher_SendEmailAndErrorLogging(t *testing.T) {
	dispatcher := NewAlertDispatcher(nil)

	cfg := EmailConfig{
		Enabled:  true,
		Host:     "127.0.0.1",
		Port:     1, // Unreachable port to trigger connection error and exercise logging
		Username: "user@example.com",
		Password: "password",
		From:     "noreply@example.com",
		To:       "admin@example.com",
	}

	alert := AlertPayload{
		Level:     "WARNING",
		Title:     "Degraded Link",
		Message:   "RSRP below threshold",
		Timestamp: time.Now(),
	}

	// Direct call to sendEmail should attempt SMTP, fail gracefully, and log error
	dispatcher.sendEmail(cfg, alert)
}
