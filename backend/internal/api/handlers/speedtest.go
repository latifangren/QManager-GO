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

const (
	speedtestPidFile      = "/tmp/qmanager_speedtest.pid"
	speedtestProgressFile = "/tmp/qmanager_speedtest_progress.json"
	speedtestResultFile   = "/tmp/qmanager_speedtest_result.json"
	speedtestErrorFile    = "/tmp/qmanager_speedtest_error"
)

// SpeedtestHandler manages speedtest execution and progress reporting.
type SpeedtestHandler struct {
	mu      sync.Mutex
	running bool
	cmd     *exec.Cmd
}

// NewSpeedtestHandler creates a SpeedtestHandler.
func NewSpeedtestHandler() *SpeedtestHandler {
	return &SpeedtestHandler{}
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
	h.mu.Unlock()

	var payload StartTestPayload
	_ = json.NewDecoder(r.Body).Decode(&payload)

	_ = os.Remove(speedtestResultFile)
	_ = os.Remove(speedtestErrorFile)
	_ = os.Remove(speedtestProgressFile)

	args := []string{"-f", "json", "-p", "yes", "--accept-license", "--accept-gdpr"}
	if payload.ServerID != nil && *payload.ServerID > 0 {
		args = append(args, "-s", strconv.Itoa(*payload.ServerID))
	}

	cmd := exec.Command("speedtest", args...)
	h.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
		Error(w, http.StatusInternalServerError, "Failed to start speedtest stdout pipe")
		return
	}

	if err := cmd.Start(); err != nil {
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to spawn speedtest: %v", err))
		return
	}

	_ = os.WriteFile(speedtestPidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)

	go func() {
		defer func() {
			h.mu.Lock()
			h.running = false
			h.cmd = nil
			h.mu.Unlock()
			_ = os.Remove(speedtestPidFile)
		}()

		scanner := bufio.NewScanner(stdout)
		var lastResult string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			// Write line to progress file
			_ = os.WriteFile(speedtestProgressFile, []byte(line), 0644)

			// Check if this line is the final result object (type: "result")
			if strings.Contains(line, `"type":"result"`) {
				lastResult = line
			}
		}

		_ = cmd.Wait()

		if lastResult != "" {
			_ = os.WriteFile(speedtestResultFile, []byte(lastResult), 0644)
		} else {
			_ = os.WriteFile(speedtestErrorFile, []byte("Speedtest terminated without result"), 0644)
		}
	}()

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Speedtest started",
	})
}

// GetStatus handles GET /api/v1/diagnostics/speedtest/status and /cgi-bin/quecmanager/at_cmd/speedtest_status.sh
func (h *SpeedtestHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	running := h.running
	h.mu.Unlock()

	if running {
		var progressObj interface{}
		if data, err := os.ReadFile(speedtestProgressFile); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &progressObj)
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"status":   "running",
			"progress": progressObj,
		})
		return
	}

	if errData, err := os.ReadFile(speedtestErrorFile); err == nil && len(errData) > 0 {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "error",
			"error":  string(errData),
		})
		return
	}

	if resData, err := os.ReadFile(speedtestResultFile); err == nil && len(resData) > 0 {
		var resultObj interface{}
		if err := json.Unmarshal(resData, &resultObj); err == nil {
			JSON(w, http.StatusOK, map[string]interface{}{
				"status": "complete",
				"result": resultObj,
			})
			return
		}
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

	_ = os.Remove(speedtestPidFile)
	h.running = false
	h.cmd = nil

	Success(w, map[string]string{"message": "Speedtest stopped"})
}
