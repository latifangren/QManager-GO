package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

// SpeedtestHandler manages native pure Go speedtest execution and progress reporting in-memory (RAM-First).
type SpeedtestHandler struct {
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	status   string      // "idle", "running", "complete", "error"
	phase    string      // "initializing", "ping", "download", "upload"
	progress interface{} // parsed json object or map
	result   interface{} // parsed final result json object
	err      string
}

// NewSpeedtestHandler creates a SpeedtestHandler.
func NewSpeedtestHandler() *SpeedtestHandler {
	return &SpeedtestHandler{
		status: "idle",
		phase:  "idle",
	}
}

// CheckAvailable handles GET /api/v1/diagnostics/speedtest/check and /cgi-bin/quecmanager/at_cmd/speedtest_check.sh
// Pure Go implementation is always available with zero external binary dependencies.
func (h *SpeedtestHandler) CheckAvailable(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
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
	client := speedtest.New()
	serverList, err := client.FetchServers()
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to fetch servers: %v", err))
		return
	}

	var servers []SpeedtestServer
	for _, s := range serverList {
		port := 8080
		if parts := strings.Split(s.Host, ":"); len(parts) == 2 {
			if p, err := strconv.Atoi(parts[1]); err == nil {
				port = p
			}
		}
		idNum, _ := strconv.Atoi(s.ID)
		servers = append(servers, SpeedtestServer{
			ID:       idNum,
			Name:     s.Name,
			Location: s.Name,
			Country:  s.Country,
			Host:     s.Host,
			Port:     port,
		})
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"servers": servers,
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

	var payload StartTestPayload
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.running = true
	h.status = "running"
	h.phase = "initializing"
	h.cancel = cancel
	h.progress = nil
	h.result = nil
	h.err = ""
	h.mu.Unlock()

	go func(serverID *int) {
		defer func() {
			h.mu.Lock()
			h.running = false
			h.cancel = nil
			if h.status == "running" {
				if h.err != "" {
					h.status = "error"
				} else if h.result != nil {
					h.status = "complete"
				} else {
					h.status = "idle"
				}
			}
			h.mu.Unlock()
		}()

		client := speedtest.New(
			speedtest.WithUserConfig(&speedtest.UserConfig{
				SavingMode: true,
			}),
		)

		user, userErr := client.FetchUserInfoContext(ctx)
		isp := "Cellular / Mobile Network"
		extIP := "127.0.0.1"
		if userErr == nil && user != nil {
			if user.Isp != "" {
				isp = user.Isp
			}
			if user.IP != "" {
				extIP = user.IP
			}
		}

		serverList, err := client.FetchServerListContext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				h.mu.Lock()
				h.status = "error"
				h.err = fmt.Sprintf("Failed to fetch server list: %v", err)
				h.mu.Unlock()
			}
			return
		}

		var targetServer *speedtest.Server
		if serverID != nil && *serverID > 0 {
			sIDStr := strconv.Itoa(*serverID)
			for _, s := range serverList {
				if s.ID == sIDStr {
					targetServer = s
					break
				}
			}
		}

		if targetServer == nil {
			targets, findErr := serverList.FindServer([]int{})
			if findErr != nil || len(targets) == 0 {
				if ctx.Err() == nil {
					h.mu.Lock()
					h.status = "error"
					h.err = "No speedtest servers available"
					h.mu.Unlock()
				}
				return
			}
			targetServer = targets[0]
		}

		sPort := 8080
		sHostOnly := targetServer.Host
		if parts := strings.Split(targetServer.Host, ":"); len(parts) == 2 {
			sHostOnly = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil {
				sPort = p
			}
		}
		sIDNum, _ := strconv.Atoi(targetServer.ID)

		ifaceInfo := map[string]interface{}{
			"internalIp": "192.168.1.1",
			"name":       "bridge0",
			"macAddr":    "00:00:00:00:00:00",
			"isVpn":      false,
			"externalIp": extIP,
		}

		serverInfo := map[string]interface{}{
			"id":       sIDNum,
			"name":     targetServer.Name,
			"location": targetServer.Name,
			"country":  targetServer.Country,
			"host":     targetServer.Host,
			"port":     sPort,
			"ip":       sHostOnly,
		}

		// Initial testStart
		h.mu.Lock()
		h.phase = "initializing"
		h.progress = map[string]interface{}{
			"type":      "testStart",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"isp":       isp,
			"interface": ifaceInfo,
			"server":    serverInfo,
		}
		h.mu.Unlock()

		if ctx.Err() != nil {
			return
		}

		// Phase: ping
		h.mu.Lock()
		h.phase = "ping"
		h.mu.Unlock()

		pingCount := 0
		_ = targetServer.PingTestContext(ctx, func(latency time.Duration) {
			pingCount++
			latMs := float64(latency.Microseconds()) / 1000.0
			prog := float64(pingCount) / 10.0
			if prog > 1.0 {
				prog = 1.0
			}
			h.mu.Lock()
			h.phase = "ping"
			h.progress = map[string]interface{}{
				"type":      "ping",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"ping": map[string]interface{}{
					"jitter":   float64(targetServer.Jitter.Microseconds()) / 1000.0,
					"latency":  latMs,
					"progress": prog,
				},
			}
			h.mu.Unlock()
		})

		if ctx.Err() != nil {
			return
		}

		h.mu.Lock()
		h.progress = map[string]interface{}{
			"type":      "ping",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"ping": map[string]interface{}{
				"jitter":   float64(targetServer.Jitter.Microseconds()) / 1000.0,
				"latency":  float64(targetServer.Latency.Microseconds()) / 1000.0,
				"progress": 1.0,
			},
		}
		h.mu.Unlock()

		time.Sleep(100 * time.Millisecond)

		// Phase: download
		if ctx.Err() != nil {
			return
		}

		h.mu.Lock()
		h.phase = "download"
		h.mu.Unlock()

		testDuration := 6 * time.Second
		targetServer.Context.SetCaptureTime(testDuration)

		dlStart := time.Now()
		targetServer.Context.SetCallbackDownload(func(rate speedtest.ByteRate) {
			elapsed := time.Since(dlStart)
			prog := float64(elapsed) / float64(testDuration)
			if prog > 1.0 {
				prog = 1.0
			}
			bytesCount := int64(float64(rate) * elapsed.Seconds())

			h.mu.Lock()
			h.phase = "download"
			h.progress = map[string]interface{}{
				"type":      "download",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"download": map[string]interface{}{
					"bandwidth": float64(rate),
					"bytes":     bytesCount,
					"elapsed":   elapsed.Milliseconds(),
					"latency": map[string]interface{}{
						"iqm": float64(targetServer.Latency.Microseconds()) / 1000.0,
					},
					"progress": prog,
				},
			}
			h.mu.Unlock()
		})

		dlErr := targetServer.DownloadTestContext(ctx)
		if dlErr != nil && ctx.Err() != nil {
			return
		}
		dlElapsed := time.Since(dlStart)

		// Phase: upload
		if ctx.Err() != nil {
			return
		}

		h.mu.Lock()
		h.phase = "upload"
		h.mu.Unlock()

		ulStart := time.Now()
		targetServer.Context.SetCallbackUpload(func(rate speedtest.ByteRate) {
			elapsed := time.Since(ulStart)
			prog := float64(elapsed) / float64(testDuration)
			if prog > 1.0 {
				prog = 1.0
			}
			bytesCount := int64(float64(rate) * elapsed.Seconds())

			h.mu.Lock()
			h.phase = "upload"
			h.progress = map[string]interface{}{
				"type":      "upload",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"upload": map[string]interface{}{
					"bandwidth": float64(rate),
					"bytes":     bytesCount,
					"elapsed":   elapsed.Milliseconds(),
					"latency": map[string]interface{}{
						"iqm": float64(targetServer.Latency.Microseconds()) / 1000.0,
					},
					"progress": prog,
				},
			}
			h.mu.Unlock()
		})

		ulErr := targetServer.UploadTestContext(ctx)
		if ulErr != nil && ctx.Err() != nil {
			return
		}
		ulElapsed := time.Since(ulStart)

		// Phase: complete / final result
		h.mu.Lock()
		h.phase = "complete"
		h.status = "complete"
		h.result = map[string]interface{}{
			"type":      "result",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"ping": map[string]interface{}{
				"jitter":  float64(targetServer.Jitter.Microseconds()) / 1000.0,
				"latency": float64(targetServer.Latency.Microseconds()) / 1000.0,
				"low":     float64(targetServer.MinLatency.Microseconds()) / 1000.0,
				"high":    float64(targetServer.MaxLatency.Microseconds()) / 1000.0,
			},
			"download": map[string]interface{}{
				"bandwidth": float64(targetServer.DLSpeed),
				"bytes":     int64(float64(targetServer.DLSpeed) * dlElapsed.Seconds()),
				"elapsed":   dlElapsed.Milliseconds(),
				"latency": map[string]interface{}{
					"iqm":    float64(targetServer.Latency.Microseconds()) / 1000.0,
					"low":    float64(targetServer.MinLatency.Microseconds()) / 1000.0,
					"high":   float64(targetServer.MaxLatency.Microseconds()) / 1000.0,
					"jitter": float64(targetServer.Jitter.Microseconds()) / 1000.0,
				},
			},
			"upload": map[string]interface{}{
				"bandwidth": float64(targetServer.ULSpeed),
				"bytes":     int64(float64(targetServer.ULSpeed) * ulElapsed.Seconds()),
				"elapsed":   ulElapsed.Milliseconds(),
				"latency": map[string]interface{}{
					"iqm":    float64(targetServer.Latency.Microseconds()) / 1000.0,
					"low":    float64(targetServer.MinLatency.Microseconds()) / 1000.0,
					"high":   float64(targetServer.MaxLatency.Microseconds()) / 1000.0,
					"jitter": float64(targetServer.Jitter.Microseconds()) / 1000.0,
				},
			},
			"packetLoss": 0.0,
			"isp":        isp,
			"interface":  ifaceInfo,
			"server":     serverInfo,
			"result": map[string]interface{}{
				"id":        fmt.Sprintf("qmanager-%d", time.Now().Unix()),
				"url":       "",
				"persisted": false,
			},
		}
		h.progress = nil
		h.mu.Unlock()
	}(payload.ServerID)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"pid":     1,
	})
}

// GetStatus handles GET /api/v1/diagnostics/speedtest/status and /cgi-bin/quecmanager/at_cmd/speedtest_status.sh
func (h *SpeedtestHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.running || h.status == "running" {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status":   "running",
			"phase":    h.phase,
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

	if h.cancel != nil {
		h.cancel()
	}

	h.running = false
	h.status = "idle"
	h.phase = "idle"
	h.progress = nil

	Success(w, map[string]string{"message": "Speedtest stopped"})
}
