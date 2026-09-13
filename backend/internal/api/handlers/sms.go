package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"qmanager/internal/atengine"
	"qmanager/internal/telemetry"
)

// SMSCenterResponse represents the response format for GET /cellular/sms.sh and /api/v1/cellular/sms.
type SMSCenterResponse struct {
	Success  bool                   `json:"success"`
	Messages []telemetry.SMSMessage `json:"messages"`
	Storage  telemetry.SMSStorage   `json:"storage"`
	Error    string                 `json:"error,omitempty"`
	Detail   string                 `json:"detail,omitempty"`
}

// SMSHandler handles native in-process SMS reading, sending, and deletion via AT Engine.
type SMSHandler struct {
	engine      *atengine.Engine
	smsToolPath string
	atDevice    string
	mu          sync.Mutex
}

// NewSMSHandler creates a new SMSHandler.
func NewSMSHandler(eng *atengine.Engine) *SMSHandler {
	return &SMSHandler{
		engine:   eng,
		atDevice: "/dev/smd11",
	}
}

// GetSMSCenter handles GET requests for SMS messages and storage status.
func (h *SMSHandler) GetSMSCenter(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	messages, storage, err := telemetry.FetchInboxAndStorage(r.Context(), "", h.atDevice, h.engine)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SMSCenterResponse{
			Success:  false,
			Messages: []telemetry.SMSMessage{},
			Storage:  telemetry.SMSStorage{Used: 0, Total: 0},
			Error:    "fetch_failed",
			Detail:   err.Error(),
		})
		return
	}

	if messages == nil {
		messages = []telemetry.SMSMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SMSCenterResponse{
		Success:  true,
		Messages: messages,
		Storage:  storage,
	})
}

// ListSMS handles GET /api/v1/cellular/sms
func (h *SMSHandler) ListSMS(w http.ResponseWriter, r *http.Request) {
	h.GetSMSCenter(w, r)
}

// HandleSMSAction handles POST /cgi-bin/quecmanager/cellular/sms.sh and /api/v1/cellular/sms
func (h *SMSHandler) HandleSMSAction(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Action  string `json:"action"`
		Phone   string `json:"phone"`
		Message string `json:"message"`
		Storage string `json:"storage"`
		Indexes []int  `json:"indexes"`
		Index   *int   `json:"index"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "send":
		h.handleSend(w, r.Context(), payload.Phone, payload.Message)
	case "delete":
		var idxs []int
		if len(payload.Indexes) > 0 {
			idxs = payload.Indexes
		} else if payload.Index != nil {
			idxs = []int{*payload.Index}
		}
		h.handleDelete(w, r.Context(), payload.Storage, idxs)
	case "delete_all":
		h.handleDeleteAll(w, r.Context())
	default:
		Error(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", payload.Action))
	}
}

// SendSMS handles POST /api/v1/cellular/sms/send
func (h *SMSHandler) SendSMS(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var req struct {
		Phone   string `json:"phone"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Phone == "" || req.Message == "" {
		Error(w, http.StatusBadRequest, "Phone number and message are required")
		return
	}

	h.handleSend(w, r.Context(), req.Phone, req.Message)
}

// DeleteSMS handles DELETE /api/v1/cellular/sms
func (h *SMSHandler) DeleteSMS(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	storage := r.URL.Query().Get("storage")
	if storage == "" {
		storage = "ME"
	}
	storage = strings.ToUpper(storage)

	idxStr := r.URL.Query().Get("index")
	if idxStr == "all" {
		h.handleDeleteAll(w, r.Context())
		return
	}

	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		Error(w, http.StatusBadRequest, "Valid 'index' parameter required")
		return
	}

	h.handleDelete(w, r.Context(), storage, []int{idx})
}

func (h *SMSHandler) handleSend(w http.ResponseWriter, ctx context.Context, rawPhone, message string) {
	phone := telemetry.NormalizePhoneNumber(rawPhone, "")
	if phone == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "missing_phone",
			"detail":  "phone number is required",
		})
		return
	}
	if strings.TrimSpace(message) == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "missing_message",
			"detail":  "message text is required",
		})
		return
	}

	if h.engine != nil {
		cleanPhone := strings.TrimPrefix(phone, "+")
		_, _ = h.engine.ExecContext(ctx, "AT+CMGF=1")
		cmgsCmd := fmt.Sprintf("AT+CMGS=\"%s\"\r%s\x1A", cleanPhone, message)
		res, err := h.engine.ExecContext(ctx, cmgsCmd)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "send_failed",
				"detail":  fmt.Sprintf("%v", err),
			})
			return
		}
		if strings.Contains(res.Raw, "ERROR") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "send_failed",
				"detail":  res.Raw,
			})
			return
		}
		Success(w, map[string]interface{}{
			"success": true,
			"detail":  "SMS sent successfully",
		})
		return
	}

	Error(w, http.StatusInternalServerError, "No SMS transport available")
}

func (h *SMSHandler) handleDelete(w http.ResponseWriter, ctx context.Context, storage string, indexes []int) {
	if len(indexes) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "missing_indexes",
			"detail":  "indexes array is required",
		})
		return
	}

	if storage != "SM" {
		storage = "ME"
	}

	if h.engine != nil {
		_, _ = h.engine.ExecContext(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, storage, storage, storage))
		for _, idx := range indexes {
			delCmd := fmt.Sprintf("AT+CMGD=%d", idx)
			_, _ = h.engine.ExecContext(ctx, delCmd)
		}
		Success(w, map[string]interface{}{"success": true})
		return
	}

	Error(w, http.StatusInternalServerError, "No SMS transport available")
}

func (h *SMSHandler) handleDeleteAll(w http.ResponseWriter, ctx context.Context) {
	if h.engine != nil {
		for _, storage := range []string{"ME", "SM"} {
			_, _ = h.engine.ExecContext(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, storage, storage, storage))
			_, _ = h.engine.ExecContext(ctx, "AT+CMGD=1,4")
		}
		Success(w, map[string]interface{}{"success": true})
		return
	}

	Error(w, http.StatusInternalServerError, "No SMS transport available")
}
