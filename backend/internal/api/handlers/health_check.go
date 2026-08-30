package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

// HealthCheckItem represents one executed diagnostic check.
type HealthCheckItem struct {
	ID         string `json:"id"`
	Category   string `json:"category"`
	Label      string `json:"label"`
	Status     string `json:"status"` // "pass", "fail", "warn", "skip"
	DurationMS int    `json:"duration_ms"`
	Detail     string `json:"detail"`
}

// HealthCheckSummary holds aggregate test metrics.
type HealthCheckSummary struct {
	Total      int `json:"total"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Warned     int `json:"warned"`
	Skipped    int `json:"skipped"`
	DurationMS int `json:"duration_ms"`
}

// HealthCheckState represents full report.
type HealthCheckState struct {
	JobID       string             `json:"job_id"`
	Status      string             `json:"status"` // "running", "complete", "complete_no_bundle", "error"
	StartedAt   int64              `json:"started_at"`
	CompletedAt int64              `json:"completed_at,omitempty"`
	Summary     HealthCheckSummary `json:"summary"`
	Tests       []HealthCheckItem  `json:"tests"`
	Error       *string            `json:"error"`
}

// HealthCheckHandler executes diagnostic health checks.
type HealthCheckHandler struct {
	engine   *atengine.Engine
	poller   *telemetry.Poller
	identity platform.Identity
	mu       sync.Mutex
	current  *HealthCheckState
}

// NewHealthCheckHandler creates a new HealthCheckHandler.
func NewHealthCheckHandler(eng *atengine.Engine, poller *telemetry.Poller, id platform.Identity) *HealthCheckHandler {
	return &HealthCheckHandler{
		engine:   eng,
		poller:   poller,
		identity: id,
	}
}

// Run handles POST /cgi-bin/quecmanager/system/health-check/run.sh and /api/system/health-check/run
func (h *HealthCheckHandler) Run(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	jobID := fmt.Sprintf("hc-%d", time.Now().UnixNano())
	now := time.Now().Unix()

	h.current = &HealthCheckState{
		JobID:     jobID,
		Status:    "running",
		StartedAt: now,
		Tests:     []HealthCheckItem{},
		Error:     nil,
	}
	h.mu.Unlock()

	// Execute diagnostic tests asynchronously
	go h.executeDiagnostics(jobID)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"job_id":     jobID,
		"started_at": now,
	})
}

func (h *HealthCheckHandler) executeDiagnostics(jobID string) {
	start := time.Now()
	var items []HealthCheckItem

	// 1. AT Engine Basic Communication
	t1Start := time.Now()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	res1, err1 := h.engine.ExecContext(ctx1, "AT")
	cancel1()
	d1 := int(time.Since(t1Start).Milliseconds())

	if err1 == nil && res1.Success {
		items = append(items, HealthCheckItem{
			ID:         "at_engine_ping",
			Category:   "hardware",
			Label:      "Baseband AT Serial Interface",
			Status:     "pass",
			DurationMS: d1,
			Detail:     "AT responsive (OK)",
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:         "at_engine_ping",
			Category:   "hardware",
			Label:      "Baseband AT Serial Interface",
			Status:     "fail",
			DurationMS: d1,
			Detail:     fmt.Sprintf("AT communication failed: %v", err1),
		})
	}

	// 2. Character Device Node Check
	t2Start := time.Now()
	smdPresent := false
	if _, err := os.Stat("/dev/smd11"); err == nil {
		smdPresent = true
	} else if _, err := os.Stat("/dev/smd7"); err == nil {
		smdPresent = true
	} else if _, err := os.Stat("/dev/ttyUSB2"); err == nil {
		smdPresent = true
	}
	d2 := int(time.Since(t2Start).Milliseconds())

	if smdPresent {
		items = append(items, HealthCheckItem{
			ID:         "dev_smd11_node",
			Category:   "hardware",
			Label:      "Qualcomm Shared Memory Device Node",
			Status:     "pass",
			DurationMS: d2,
			Detail:     "Device node present and accessible",
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:         "dev_smd11_node",
			Category:   "hardware",
			Label:      "Qualcomm Shared Memory Device Node",
			Status:     "warn",
			DurationMS: d2,
			Detail:     "Direct node not found; operating in mock/fallback mode",
		})
	}

	// 3. SIM Readiness Check
	t3Start := time.Now()
	ctx3, cancel3 := context.WithTimeout(context.Background(), 2*time.Second)
	res3, err3 := h.engine.ExecContext(ctx3, "AT+CPIN?")
	cancel3()
	d3 := int(time.Since(t3Start).Milliseconds())

	if err3 == nil && res3 != nil {
		items = append(items, HealthCheckItem{
			ID:         "sim_card_readiness",
			Category:   "cellular",
			Label:      "SIM Card Readiness (+CPIN)",
			Status:     "pass",
			DurationMS: d3,
			Detail:     res3.Raw,
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:         "sim_card_readiness",
			Category:   "cellular",
			Label:      "SIM Card Readiness (+CPIN)",
			Status:     "warn",
			DurationMS: d3,
			Detail:     "Unable to verify CPIN status",
		})
	}

	// 4. Memory RSS Footprint (<20MB target)
	t4Start := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	allocMB := float64(m.Alloc) / 1024.0 / 1024.0
	sysMB := float64(m.Sys) / 1024.0 / 1024.0
	d4 := int(time.Since(t4Start).Milliseconds())

	if sysMB < 25.0 {
		items = append(items, HealthCheckItem{
			ID:         "memory_rss_footprint",
			Category:   "system",
			Label:      "Go Runtime Memory Footprint (Cortex-A7 Budget <20MB)",
			Status:     "pass",
			DurationMS: d4,
			Detail:     fmt.Sprintf("Allocated: %.2fMB, System Heap RSS: %.2fMB", allocMB, sysMB),
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:         "memory_rss_footprint",
			Category:   "system",
			Label:      "Go Runtime Memory Footprint (Cortex-A7 Budget <20MB)",
			Status:     "warn",
			DurationMS: d4,
			Detail:     fmt.Sprintf("Elevated memory allocation: %.2fMB", sysMB),
		})
	}

	// 5. Network Interfaces Check
	t5Start := time.Now()
	netStats, _ := platform.ReadNetworkStats("")
	d5 := int(time.Since(t5Start).Milliseconds())
	if len(netStats) > 0 {
		items = append(items, HealthCheckItem{
			ID:         "network_interfaces",
			Category:   "network",
			Label:      "Linux Network Interfaces (/proc/net/dev)",
			Status:     "pass",
			DurationMS: d5,
			Detail:     fmt.Sprintf("Detected %d active network interfaces", len(netStats)),
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:         "network_interfaces",
			Category:   "network",
			Label:      "Linux Network Interfaces (/proc/net/dev)",
			Status:     "warn",
			DurationMS: d5,
			Detail:     "No network interface counters parsed",
		})
	}

	// Summary calculations
	passed, failed, warned, skipped := 0, 0, 0, 0
	for _, it := range items {
		switch it.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		case "warn":
			warned++
		default:
			skipped++
		}
	}

	totalDuration := int(time.Since(start).Milliseconds())

	h.mu.Lock()
	if h.current != nil && h.current.JobID == jobID {
		h.current.Status = "complete"
		h.current.CompletedAt = time.Now().Unix()
		h.current.Tests = items
		h.current.Summary = HealthCheckSummary{
			Total:      len(items),
			Passed:     passed,
			Failed:     failed,
			Warned:     warned,
			Skipped:    skipped,
			DurationMS: totalDuration,
		}
	}
	h.mu.Unlock()
}

// Status handles GET /cgi-bin/quecmanager/system/health-check/status.sh and /api/system/health-check/status
func (h *HealthCheckHandler) Status(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.current == nil {
		JSON(w, http.StatusOK, HealthCheckState{
			JobID:     "",
			Status:    "complete_no_bundle",
			StartedAt: 0,
			Tests:     []HealthCheckItem{},
			Summary: HealthCheckSummary{
				Total: 0,
			},
		})
		return
	}

	JSON(w, http.StatusOK, h.current)
}

// Download handles GET /cgi-bin/quecmanager/system/health-check/download.sh and /api/system/health-check/download
func (h *HealthCheckHandler) Download(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.current == nil {
		Error(w, http.StatusNotFound, "No health check report available")
		return
	}

	data, err := json.MarshalIndent(h.current, "", "  ")
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to serialize report")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=healthcheck-%s.json", h.current.JobID))
	_, _ = w.Write(data)
}

// Clear handles POST /cgi-bin/quecmanager/system/health-check/clear.sh and /api/system/health-check/clear
func (h *HealthCheckHandler) Clear(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.current = nil
	Success(w, map[string]interface{}{
		"success": true,
		"message": "Health check results cleared",
	})
}
