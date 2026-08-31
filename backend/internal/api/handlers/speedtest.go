package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SpeedtestHandler manages speedtest execution and progress reporting in-memory (RAM-First).
type SpeedtestHandler struct {
	mu       sync.RWMutex
	running  bool
	cmd      *exec.Cmd
	status   string      // "idle", "running", "complete", "error"
	progress interface{} // parsed json object or map
	result   interface{} // parsed final result json object
	err      string
}

// NewSpeedtestHandler creates a SpeedtestHandler.
func NewSpeedtestHandler() *SpeedtestHandler {
	return &SpeedtestHandler{
		status: "idle",
	}
}

// CheckAvailable handles GET /api/v1/diagnostics/speedtest/check and /cgi-bin/quecmanager/at_cmd/speedtest_check.sh
func (h *SpeedtestHandler) CheckAvailable(w http.ResponseWriter, r *http.Request) {
	_, err := exec.LookPath("speedtest")
	JSON(w, http.StatusOK, map[string]interface{}{
		"available": err == nil,
	})
}

// SpeedtestServer matches Ookla server structure.
type SpeedtestServer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Country  string `json:"country"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// ListServers handles GET /api/v1/diagnostics/speedtest/servers and /cgi-bin/quecmanager/at_cmd/speedtest_servers.sh
func (h *SpeedtestHandler) ListServers(w http.ResponseWriter, r *http.Request) {
	if _, err := exec.LookPath("speedtest"); err != nil {
		Error(w, http.StatusNotFound, "speedtest-cli binary not found")
		return
	}

	cmd := exec.Command("speedtest", "--servers", "-f", "json", "--accept-license", "--accept-gdpr")
	out, err := cmd.Output()
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to retrieve speedtest servers")
		return
	}

	var parsed struct {
		Servers []SpeedtestServer `json:"servers"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to parse speedtest servers JSON")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"servers": parsed.Servers,
	})
}

// StartTestPayload represents start parameters.
type StartTestPayload struct {
	ServerID *int `json:"server_id,omitempty"`
}

// StartTest handles POST /api/v1/diagnostics/speedtest/start and /cgi-bin/quecmanager/at_cmd/speedtest_start.sh
func (h *SpeedtestHandler) StartTest(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		Error(w, http.StatusConflict, "Speedtest already running")
		return
	}
	h.running = true
	h.status = "running"
	h.progress = nil
	h.result = nil
	h.err = ""
	h.mu.Unlock()

	var payload StartTestPayload
	_ = json.NewDecoder(r.Body).Decode(&payload)

	args := []string{"-f", "json", "-p", "yes", "--accept-license", "--accept-gdpr"}
	if payload.ServerID != nil && *payload.ServerID > 0 {
		args = append(args, "-s", strconv.Itoa(*payload.ServerID))
	}

	cmd := exec.Command("speedtest", args...)
	h.mu.Lock()
	h.cmd = cmd
	h.mu.Unlock()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.mu.Lock()
		h.running = false
		h.status = "error"
		h.err = "Failed to start speedtest stdout pipe"
		h.cmd = nil
		h.mu.Unlock()
		Error(w, http.StatusInternalServerError, "Failed to start speedtest stdout pipe")
		return
	}

	if err := cmd.Start(); err != nil {
		h.mu.Lock()
		h.running = false
		h.status = "error"
		h.err = fmt.Sprintf("Failed to spawn speedtest: %v", err)
		h.cmd = nil
		h.mu.Unlock()
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to spawn speedtest: %v", err))
		return
	}

	go func() {
		defer func() {
			h.mu.Lock()
			h.running = false
			h.cmd = nil
			h.mu.Unlock()
		}()

		scanner := bufio.NewScanner(stdout)
		var lastResult string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			var lineObj interface{}
			if err := json.Unmarshal([]byte(line), &lineObj); err == nil {
				h.mu.Lock()
				h.progress = lineObj
				h.mu.Unlock()
			}

			// Check if this line is the final result object (type: "result")
			if strings.Contains(line, `"type":"result"`) {
				lastResult = line
			}
		}

		_ = cmd.Wait()

		h.mu.Lock()
		defer h.mu.Unlock()
		if lastResult != "" {
			var resultObj interface{}
			if err := json.Unmarshal([]byte(lastResult), &resultObj); err == nil {
				h.result = resultObj
				h.status = "complete"
				h.err = ""
			} else {
				h.result = lastResult
				h.status = "complete"
				h.err = ""
			}
		} else if h.status != "idle" {
			h.status = "error"
			h.err = "Speedtest terminated without result"
		}
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Speedtest started",
	})
}

// GetStatus handles GET /api/v1/diagnostics/speedtest/status and /cgi-bin/quecmanager/at_cmd/speedtest_status.sh
func (h *SpeedtestHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.running || h.status == "running" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status":   "running",
			"progress": h.progress,
		})
		return
	}

	if h.status == "error" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "error",
			"error":  h.err,
		})
		return
	}

	if h.status == "complete" && h.result != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "complete",
			"result": h.result,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"status": "idle",
	})
}

// StopTest handles POST /api/v1/diagnostics/speedtest/stop
func (h *SpeedtestHandler) StopTest(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Signal(syscall.SIGTERM)
		go func(p *os.Process) {
			time.Sleep(2 * time.Second)
			_ = p.Kill()
		}(h.cmd.Process)
	}

	h.running = false
	h.status = "idle"
	h.cmd = nil

	Success(w, map[string]string{"message": "Speedtest stopped"})
}
