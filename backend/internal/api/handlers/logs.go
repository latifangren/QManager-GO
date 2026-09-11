package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

const (
	MaxRotatedLogFiles = 2
)

var (
	DefaultLogFilePath  = "/tmp/qmanager.log"
	DefaultSubsysDevice = "/sys/bus/msm_subsys/devices/subsys0"
	CrashLogFile        = "/etc/qmanager/modem_crashes.json"
	RamdumpDir          = "/usrdata/ramdump_modem"
)

// LogEntry represents a single parsed log line.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Component string `json:"component"`
	PID       string `json:"pid"`
	Message   string `json:"message"`
}

// LogStats holds log size and rotation metadata.
type LogStats struct {
	CurrentSizeKB int `json:"current_size_kb"`
	CurrentLines  int `json:"current_lines"`
	RotatedFiles  int `json:"rotated_files"`
}

// LogsResponse represents the structured GET /system/logs response.
type LogsResponse struct {
	Success             bool       `json:"success"`
	Entries             []LogEntry `json:"entries"`
	Total               int        `json:"total"`
	Stats               LogStats   `json:"stats"`
	AvailableComponents []string   `json:"available_components"`
}

// CPUStats holds detailed CPU breakdown.
type CPUStats struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	ModelName    string  `json:"model_name"`
	TemperatureC float64 `json:"temperature_c"`
}

// MemoryStats holds detailed memory metrics.
type MemoryStats struct {
	TotalMB     float64 `json:"total_mb"`
	UsedMB      float64 `json:"used_mb"`
	FreeMB      float64 `json:"free_mb"`
	AvailableMB float64 `json:"available_mb"`
}

// ModemSubsysData holds diagnostic status for modem hardware subsystem.
type ModemSubsysData struct {
	State              string      `json:"state"`
	CrashCount         *int        `json:"crash_count"`
	LastCrashTimestamp *int64      `json:"last_crash_timestamp"`
	UptimeSecs         int64       `json:"uptime_secs"`
	CPU                CPUStats    `json:"cpu"`
	Memory             MemoryStats `json:"memory"`
	SubsysName         string      `json:"subsys_name"`
	FirmwareVersion    string      `json:"firmware_version"`
}

// LogsHandler implements /system/logs and /system/modem-subsys.
type LogsHandler struct {
	ringLogger   *telemetry.RingBufferLogger
	logFilePath  string
	subsysPath   string
	crashLogPath string
	ramdumpDir   string
	mu           sync.Mutex
}

// NewLogsHandler initializes a LogsHandler.
func NewLogsHandler() *LogsHandler {
	logPath := os.Getenv("QMANAGER_LOG_FILE")
	if logPath == "" {
		logPath = DefaultLogFilePath
	}
	subsys := os.Getenv("QMANAGER_SUBSYS_PATH")
	if subsys == "" {
		subsys = DefaultSubsysDevice
	}
	crashLog := os.Getenv("QMANAGER_CRASH_LOG")
	if crashLog == "" {
		crashLog = CrashLogFile
	}
	ramdump := os.Getenv("QMANAGER_RAMDUMP_DIR")
	if ramdump == "" {
		ramdump = RamdumpDir
	}

	return &LogsHandler{
		ringLogger:   telemetry.GetGlobalLogger(),
		logFilePath:  logPath,
		subsysPath:   subsys,
		crashLogPath: crashLog,
		ramdumpDir:   ramdump,
	}
}

var syslogRe = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d+\s+\d{2}:\d{2}:\d{2})\s+(?:[^\s]+\s+)?(?:([a-z]+\.[a-z]+)\s+)?([a-zA-Z0-9_.-]+)(?:\[(\d+)\])?:\s+(.*)$`)
var bracketLogRe = regexp.MustCompile(`^(?:(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+)?\[(.*?)\]\s+\[(.*?)(?::(.*?))?\]\s+(.*)$`)

// parseLogLine parses a line from log format into LogEntry.
func parseLogLine(line string) (LogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return LogEntry{}, false
	}

	// 1. Match bracket format: "2026-09-11 14:00:00 [INFO] [qmanager] Message"
	if m := bracketLogRe.FindStringSubmatch(line); len(m) == 6 {
		ts := m[1]
		if ts == "" {
			ts = time.Now().Format("2006-01-02 15:04:05")
		}
		pid := m[4]
		if pid == "" {
			pid = "-"
		}
		return LogEntry{
			Timestamp: ts,
			Level:     strings.ToUpper(m[2]),
			Component: m[3],
			PID:       pid,
			Message:   m[5],
		}, true
	}

	// 2. Match standard Linux /var/log/messages syslog format
	if m := syslogRe.FindStringSubmatch(line); len(m) == 6 {
		tsStr := m[1]
		facLevel := m[2]
		comp := m[3]
		pid := m[4]
		msg := m[5]

		if pid == "" {
			pid = "-"
		}

		// Normalize timestamp to YYYY-MM-DD HH:MM:SS
		ts := time.Now().Format("2006-01-02 15:04:05")
		if parsed, err := time.Parse("Jan 2 15:04:05", tsStr); err == nil {
			now := time.Now()
			fullTime := time.Date(now.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.Local)
			ts = fullTime.Format("2006-01-02 15:04:05")
		}

		level := "INFO"
		lowerFac := strings.ToLower(facLevel)
		lowerMsg := strings.ToLower(msg)
		if strings.Contains(lowerFac, "err") || strings.Contains(lowerFac, "crit") || strings.Contains(lowerFac, "alert") || strings.Contains(lowerFac, "emerg") || strings.Contains(lowerMsg, "error") || strings.Contains(lowerMsg, "fatal") {
			level = "ERROR"
		} else if strings.Contains(lowerFac, "warn") || strings.Contains(lowerMsg, "warning") || strings.Contains(lowerMsg, "warn") {
			level = "WARN"
		} else if strings.Contains(lowerFac, "debug") || strings.Contains(lowerMsg, "debug") {
			level = "DEBUG"
		}

		return LogEntry{
			Timestamp: ts,
			Level:     level,
			Component: comp,
			PID:       pid,
			Message:   msg,
		}, true
	}

	return LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Level:     "INFO",
		Component: "system",
		PID:       "-",
		Message:   line,
	}, true
}

// GetLogs handles GET /api/v1/system/logs and GET /cgi-bin/quecmanager/system/logs.sh
func (h *LogsHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	linesParam := r.URL.Query().Get("lines")
	levelParam := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("level")))
	componentParam := strings.TrimSpace(r.URL.Query().Get("component"))
	searchParam := strings.TrimSpace(r.URL.Query().Get("search"))
	includeRotated := r.URL.Query().Get("include_rotated") == "1"

	maxLines := 100
	if l, err := strconv.Atoi(linesParam); err == nil && l > 0 {
		maxLines = l
	}

	var allEntries []LogEntry
	compMap := make(map[string]bool)

	// 1. Gather all in-memory ring records (QManager Go daemon logs)
	if h.ringLogger != nil {
		allRecords := h.ringLogger.GetRecords(0, "", "", "")
		for _, rec := range allRecords {
			src := rec.Source
			if src == "" {
				src = "qmanager"
			}
			compMap[src] = true

			allEntries = append(allEntries, LogEntry{
				Timestamp: rec.Timestamp.Format("2006-01-02 15:04:05"),
				Level:     string(rec.Level),
				Component: src,
				PID:       "qmanager",
				Message:   rec.Message,
			})
		}
	}

	// 2. Gather system syslog files (/var/log/messages, /tmp/qmanager.log)
	sources := h.getLogSources(includeRotated)
	for _, src := range sources {
		if f, err := os.Open(src); err == nil {
			scanner := bufio.NewScanner(f)
			// Read up to 2000 lines per file
			count := 0
			for scanner.Scan() && count < 2000 {
				line := scanner.Text()
				if entry, ok := parseLogLine(line); ok {
					if entry.Component != "" {
						compMap[entry.Component] = true
					}
					allEntries = append(allEntries, entry)
				}
				count++
			}
			_ = f.Close()
		}
	}

	// 3. Sort all entries by timestamp descending (newest first)
	sort.SliceStable(allEntries, func(i, j int) bool {
		return allEntries[i].Timestamp > allEntries[j].Timestamp
	})

	// 5. Apply filters
	var filtered []LogEntry
	for _, entry := range allEntries {
		if levelParam != "" && levelParam != "ALL" && !strings.EqualFold(entry.Level, levelParam) {
			continue
		}
		if componentParam != "" && componentParam != "all" && !strings.EqualFold(entry.Component, componentParam) {
			continue
		}
		if searchParam != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(searchParam)) &&
			!strings.Contains(strings.ToLower(entry.Component), strings.ToLower(searchParam)) {
			continue
		}
		filtered = append(filtered, entry)
		if len(filtered) >= maxLines {
			break
		}
	}

	var components []string
	for k := range compMap {
		if k != "" {
			components = append(components, k)
		}
	}
	sort.Strings(components)

	stats := h.getStats()
	if h.ringLogger != nil {
		stats.CurrentLines += h.ringLogger.Count()
		stats.CurrentSizeKB += (h.ringLogger.Count() * 128) / 1024
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(LogsResponse{
		Success:             true,
		Entries:             filtered,
		Total:               len(filtered),
		Stats:               stats,
		AvailableComponents: components,
	})
}

// HandleLogsAction handles POST /api/v1/system/logs and POST /cgi-bin/quecmanager/system/logs.sh
func (h *LogsHandler) HandleLogsAction(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var payload struct {
		Action string `json:"action"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch payload.Action {
	case "clear", "clear_logs":
		if h.ringLogger != nil {
			h.ringLogger.Clear()
		}
		_ = os.Truncate(h.logFilePath, 0)
		_ = os.Truncate("/var/log/messages", 0)
		_ = os.Remove("/var/log/messages.0")
		_ = os.Truncate("/tmp/messages", 0)
		for i := 1; i <= MaxRotatedLogFiles; i++ {
			_ = os.Remove(fmt.Sprintf("%s.%d", h.logFilePath, i))
			_ = os.Remove(fmt.Sprintf("/var/log/messages.%d", i))
		}
		Success(w, map[string]interface{}{"success": true, "message": "Logs cleared"})
		return

	case "rotate", "rotate_logs":
		// Rotate /var/log/messages
		if _, err := os.Stat("/var/log/messages"); err == nil {
			_ = os.Rename("/var/log/messages", "/var/log/messages.0")
			_ = os.WriteFile("/var/log/messages", []byte{}, 0644)
		}
		if _, err := os.Stat(h.logFilePath); err == nil {
			_ = os.Rename(h.logFilePath, fmt.Sprintf("%s.1", h.logFilePath))
			_ = os.WriteFile(h.logFilePath, []byte{}, 0644)
		}
		stats := h.getStats()
		if h.ringLogger != nil {
			stats.CurrentLines += h.ringLogger.Count()
		}
		JSON(w, http.StatusOK, map[string]interface{}{"success": true, "stats": stats})
		return

	default:
		Error(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", payload.Action))
	}
}

// ModemSubsys handles GET /api/v1/system/modem-subsys and GET /cgi-bin/quecmanager/system/modem-subsys.sh
func (h *LogsHandler) ModemSubsys(w http.ResponseWriter, r *http.Request) {
	// 1. Try reading /tmp/qmanager_status.json if fresh
	if data, ok := h.readCachedSubsys(); ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
		return
	}

	// 2. Query live subsystem
	data := h.queryLiveSubsys()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (h *LogsHandler) getLogSources(includeRotated bool) []string {
	var sources []string
	candidates := []string{
		h.logFilePath,
		"/var/log/messages",
		"/tmp/messages",
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			sources = append(sources, p)
		}
		if includeRotated {
			for i := 0; i <= MaxRotatedLogFiles; i++ {
				rot := fmt.Sprintf("%s.%d", p, i)
				if _, err := os.Stat(rot); err == nil {
					sources = append(sources, rot)
				}
			}
		}
	}
	return sources
}

func (h *LogsHandler) getStats() LogStats {
	var sizeKB, lineCount, rotated int
	for _, p := range []string{h.logFilePath, "/var/log/messages"} {
		if fi, err := os.Stat(p); err == nil {
			sizeKB += int(fi.Size() / 1024)
			if f, err := os.Open(p); err == nil {
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					lineCount++
				}
				_ = f.Close()
			}
		}
	}
	for i := 0; i <= MaxRotatedLogFiles; i++ {
		for _, p := range []string{h.logFilePath, "/var/log/messages"} {
			rot := fmt.Sprintf("%s.%d", p, i)
			if _, err := os.Stat(rot); err == nil {
				rotated++
			}
		}
	}
	return LogStats{
		CurrentSizeKB: sizeKB,
		CurrentLines:  lineCount,
		RotatedFiles:  rotated,
	}
}

func (h *LogsHandler) fallbackJournalctl(maxLines int, level, search string) []LogEntry {
	cmd := exec.Command("logread", "-l", strconv.Itoa(maxLines))
	out, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("dmesg")
		out, err = cmd.Output()
		if err != nil {
			return []LogEntry{}
		}
	}

	var entries []LogEntry
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(l), strings.ToLower(search)) {
			continue
		}

		entries = append(entries, LogEntry{
			Timestamp: time.Now().Format("2006-01-02 15:04:05"),
			Level:     "INFO",
			Component: "kernel",
			PID:       "-",
			Message:   l,
		})
	}

	if len(entries) > maxLines {
		return entries[len(entries)-maxLines:]
	}
	return entries
}

func (h *LogsHandler) readCachedSubsys() (*ModemSubsysData, bool) {
	data, err := os.ReadFile("/tmp/qmanager_status.json")
	if err != nil {
		return nil, false
	}
	var cached struct {
		System  platform.SystemMetrics `json:"system"`
		Model   string                 `json:"device_model"`
		Rev     string                 `json:"revision"`
		Crashes int                    `json:"crashes"`
	}
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}

	totalMB := float64(cached.System.MemTotalKB) / 1024.0
	freeMB := float64(cached.System.MemFreeKB) / 1024.0
	availMB := float64(cached.System.MemAvailKB) / 1024.0
	crashes := cached.Crashes

	return &ModemSubsysData{
		State:              "online",
		CrashCount:         &crashes,
		LastCrashTimestamp: nil,
		UptimeSecs:         int64(cached.System.UptimeSeconds),
		CPU: CPUStats{
			UsagePercent: cached.System.CPUUsage,
			Cores:        runtime.NumCPU(),
			ModelName:    "ARM Cortex-A7 (SDX55)",
			TemperatureC: cached.System.CpuTempC,
		},
		Memory: MemoryStats{
			TotalMB:     totalMB,
			UsedMB:      totalMB - freeMB,
			FreeMB:      freeMB,
			AvailableMB: availMB,
		},
		SubsysName:      "modem",
		FirmwareVersion: cached.Rev,
	}, true
}

func (h *LogsHandler) queryLiveSubsys() *ModemSubsysData {
	metrics := platform.GetSystemMetrics()

	totalMB := float64(metrics.MemTotalKB) / 1024.0
	freeMB := float64(metrics.MemFreeKB) / 1024.0
	availMB := float64(metrics.MemAvailKB) / 1024.0

	crashes := h.countCrashes()

	return &ModemSubsysData{
		State:              "online",
		CrashCount:         &crashes,
		LastCrashTimestamp: nil,
		UptimeSecs:         int64(metrics.UptimeSeconds),
		CPU: CPUStats{
			UsagePercent: metrics.CPUUsage,
			Cores:        runtime.NumCPU(),
			ModelName:    "ARM Cortex-A7 (SDX55)",
			TemperatureC: metrics.CpuTempC,
		},
		Memory: MemoryStats{
			TotalMB:     totalMB,
			UsedMB:      totalMB - freeMB,
			FreeMB:      freeMB,
			AvailableMB: availMB,
		},
		SubsysName:      "modem",
		FirmwareVersion: "SDX55",
	}
}

func (h *LogsHandler) countCrashes() int {
	files, err := os.ReadDir(h.ramdumpDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "ramdump_") {
			count++
		}
	}
	return count
}
