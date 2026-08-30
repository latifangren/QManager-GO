package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"qmanager/internal/atengine"
)

// NetworkPriorityHandler handles RAT acquisition order settings.
type NetworkPriorityHandler struct {
	engine *atengine.Engine
}

// NewNetworkPriorityHandler creates a NetworkPriorityHandler.
func NewNetworkPriorityHandler(engine *atengine.Engine) *NetworkPriorityHandler {
	return &NetworkPriorityHandler{engine: engine}
}

// GetPriority handles GET /api/v1/cellular/priority and GET /cgi-bin/quecmanager/cellular/network_priority.sh
func (h *NetworkPriorityHandler) GetPriority(w http.ResponseWriter, r *http.Request) {
	res, err := h.engine.Exec(`AT+QNWPREFCFG="rat_acq_order"`)
	if err != nil || !strings.Contains(res.Raw, "+QNWPREFCFG:") {
		Error(w, http.StatusInternalServerError, "Failed to read RAT acquisition order")
		return
	}

	order := parseRatAcqOrder(res.Raw)
	if order == "" {
		Error(w, http.StatusInternalServerError, "Could not parse rat_acq_order response")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"rat_acq_order": order,
	})
}

// SetPriorityPayload represents POST payload for setting RAT acquisition order.
type SetPriorityPayload struct {
	Order string `json:"order"`
}

// SetPriority handles POST /api/v1/cellular/priority and POST /cgi-bin/quecmanager/cellular/network_priority.sh
func (h *NetworkPriorityHandler) SetPriority(w http.ResponseWriter, r *http.Request) {
	var p SetPriorityPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Order == "" {
		Error(w, http.StatusBadRequest, "order field is required")
		return
	}

	order := strings.TrimSpace(p.Order)
	if !isValidRatAcqOrder(order) {
		Error(w, http.StatusBadRequest, "Invalid rat_acq_order format (e.g. NR5G:LTE:WCDMA)")
		return
	}

	cmd := fmt.Sprintf(`AT+QNWPREFCFG="rat_acq_order",%s`, order)
	res, err := h.engine.Exec(cmd)
	if err != nil || !strings.Contains(res.Raw, "OK") {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to set RAT priority: %v", err))
		return
	}

	Success(w, map[string]interface{}{
		"message": "RAT acquisition order updated successfully",
		"order":   order,
	})
}

func parseRatAcqOrder(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "+QNWPREFCFG:") && strings.Contains(l, "rat_acq_order") {
			parts := strings.Split(l, ",")
			if len(parts) >= 2 {
				return strings.Trim(parts[1], "\" \r\n")
			}
		}
	}
	return ""
}

func isValidRatAcqOrder(order string) bool {
	if order == "" {
		return false
	}
	validRATs := map[string]bool{
		"NR5G":  true,
		"LTE":   true,
		"WCDMA": true,
		"GSM":   true,
		"AUTO":  true,
	}

	tokens := strings.Split(order, ":")
	for _, t := range tokens {
		t = strings.TrimSpace(strings.ToUpper(t))
		if !validRATs[t] {
			return false
		}
	}
	return true
}
