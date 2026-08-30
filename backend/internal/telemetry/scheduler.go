package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
)

// ClockSane checks if the system clock has stepped forward past the 1970 boot window (year >= 2024).
func ClockSane(now time.Time) bool {
	return now.Year() >= 2024
}

// BootSettled checks if system uptime >= settleSecs (default 300s).
func BootSettled(uptimeSecs float64, settleSecs float64) bool {
	if settleSecs <= 0 {
		settleSecs = 300
	}
	return uptimeSecs >= settleSecs
}

// NowMatchesHHMM checks if the given time matches target HH:MM within tolerance minutes.
func NowMatchesHHMM(targetHHMM string, now time.Time, toleranceMin int) bool {
	parts := strings.Split(targetHHMM, ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return false
	}

	targetMin := h*60 + m
	nowMin := now.Hour()*60 + now.Minute()

	diff := nowMin - targetMin
	if diff < 0 {
		diff = -diff
	}
	if diff > 720 {
		diff = 1440 - diff
	}
	return diff <= toleranceMin
}

// DayMatches checks if current day of week matches configured days list (0=Sun, 6=Sat).
func DayMatches(configuredDays string, dayOfWeek time.Weekday) bool {
	if configuredDays == "" {
		return true
	}
	currentDayStr := strconv.Itoa(int(dayOfWeek))
	for _, d := range strings.Split(configuredDays, ",") {
		if strings.TrimSpace(d) == currentDayStr {
			return true
		}
	}
	return false
}

// TowerConfigSnapshot mirrors tower_lock.json schedule structure for scheduler checking.
type TowerConfigSnapshot struct {
	LTE struct {
		Enabled bool `json:"enabled"`
	} `json:"lte"`
	NRSA struct {
		Enabled bool `json:"enabled"`
	} `json:"nr_sa"`
	Schedule struct {
		Enabled   bool   `json:"enabled"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Days      []int  `json:"days"`
	} `json:"schedule"`
}

// JobExecutor defines custom action hooks for scheduled jobs (useful in tests).
type JobExecutor struct {
	RebootFunc      func(ctx context.Context) error
	TowerApplyFunc  func(ctx context.Context) error
	TowerClearFunc  func(ctx context.Context) error
	AutoUpdateFunc  func(ctx context.Context) error
	GetUptimeFunc   func() float64
	GetTimeFunc     func() time.Time
}

// Scheduler handles periodic evaluation and execution of scheduled modem operations.
type Scheduler struct {
	engine     *atengine.Engine
	cfgMgr     *config.Manager
	executor   JobExecutor
	towerCfg   string
	mu         sync.Mutex
	stopCh     chan struct{}
	running    bool
	lastReboot string
	lastTower  string
	lastUpdate string
}

// NewScheduler creates a new Monotonic Scheduler.
func NewScheduler(eng *atengine.Engine, cfgMgr *config.Manager) *Scheduler {
	return &Scheduler{
		engine:   eng,
		cfgMgr:   cfgMgr,
		towerCfg: "/etc/qmanager/tower_lock.json",
		stopCh:   make(chan struct{}),
		executor: JobExecutor{
			RebootFunc: func(ctx context.Context) error {
				log.Println("[Scheduler] Executing scheduled modem reboot")
				return exec.CommandContext(ctx, "reboot").Run()
			},
			TowerApplyFunc: func(ctx context.Context) error {
				log.Println("[Scheduler] Executing tower schedule apply")
				return exec.CommandContext(ctx, "qmanager_tower_schedule", "apply").Run()
			},
			TowerClearFunc: func(ctx context.Context) error {
				log.Println("[Scheduler] Executing tower schedule clear")
				return exec.CommandContext(ctx, "qmanager_tower_schedule", "clear").Run()
			},
			AutoUpdateFunc: func(ctx context.Context) error {
				log.Println("[Scheduler] Executing scheduled auto-update check")
				return exec.CommandContext(ctx, "qmanager_update", "check").Run()
			},
			GetUptimeFunc: func() float64 {
				up, err := platform.ReadUptime("")
				if err != nil {
					return 0
				}
				return up
			},
			GetTimeFunc: func() time.Time {
				return time.Now()
			},
		},
	}
}

// SetExecutor overrides job handlers (primarily for unit tests).
func (s *Scheduler) SetExecutor(exec JobExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executor = exec
}

// SetTowerConfigPath overrides tower configuration path.
func (s *Scheduler) SetTowerConfigPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.towerCfg = path
}

// Start begins the scheduler evaluation loop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.loop()
}

// Stop halts the scheduler loop.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.Evaluate()
		}
	}
}

// Evaluate runs one check cycle across scheduled reboot, tower schedule, and auto-update.
func (s *Scheduler) Evaluate() {
	s.mu.Lock()
	getTime := s.executor.GetTimeFunc
	getUptime := s.executor.GetUptimeFunc
	s.mu.Unlock()

	now := time.Now()
	if getTime != nil {
		now = getTime()
	}

	var uptime float64
	if getUptime != nil {
		uptime = getUptime()
	} else {
		uptime, _ = platform.ReadUptime("")
	}

	// 1970 Clock-step Guard: do not trigger real calendar events if current system year < 2024
	if !ClockSane(now) {
		return
	}

	s.checkReboot(now, uptime)
	s.checkTowerSchedule(now, uptime)
	s.checkAutoUpdate(now, uptime)
}

func (s *Scheduler) checkReboot(now time.Time, uptime float64) {
	cfg := s.cfgMgr.Get().Settings
	if cfg.SchedRebootEnabled == 0 || cfg.SchedRebootTime == "" {
		return
	}

	// Guard: check day-of-week match
	if !DayMatches(cfg.SchedRebootDays, now.Weekday()) {
		return
	}

	// Guard: check time match (10 min tolerance)
	if !NowMatchesHHMM(cfg.SchedRebootTime, now, 10) {
		return
	}

	// Guard: must be settled or within schedule window; avoid multi-triggering in same day-minute slot
	slotKey := fmt.Sprintf("%s-%s", now.Format("2006-01-02"), cfg.SchedRebootTime)
	s.mu.Lock()
	if s.lastReboot == slotKey {
		s.mu.Unlock()
		return
	}
	s.lastReboot = slotKey
	rebootFn := s.executor.RebootFunc
	s.mu.Unlock()

	// Check if OTA update in progress
	if _, err := os.Stat("/tmp/qmanager_update.pid"); err == nil {
		log.Println("[Scheduler] Scheduled reboot skipped: /tmp/qmanager_update.pid exists")
		return
	}

	if rebootFn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rebootFn(ctx)
	}
}

func (s *Scheduler) checkTowerSchedule(now time.Time, uptime float64) {
	s.mu.Lock()
	cfgPath := s.towerCfg
	s.mu.Unlock()

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return
	}

	var tower TowerConfigSnapshot
	if err := json.Unmarshal(data, &tower); err != nil {
		return
	}

	if !tower.Schedule.Enabled {
		return
	}

	// Check day match
	if len(tower.Schedule.Days) > 0 {
		dayMatch := false
		curDay := int(now.Weekday())
		for _, d := range tower.Schedule.Days {
			if d == curDay {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return
		}
	}

	// 1. Check Apply time
	if tower.Schedule.StartTime != "" && NowMatchesHHMM(tower.Schedule.StartTime, now, 10) {
		slotKey := fmt.Sprintf("apply-%s-%s", now.Format("2006-01-02"), tower.Schedule.StartTime)
		s.mu.Lock()
		if s.lastTower != slotKey {
			s.lastTower = slotKey
			applyFn := s.executor.TowerApplyFunc
			s.mu.Unlock()

			if applyFn != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = applyFn(ctx)
			}
		} else {
			s.mu.Unlock()
		}
		return
	}

	// 2. Check Clear time
	if tower.Schedule.EndTime != "" && NowMatchesHHMM(tower.Schedule.EndTime, now, 10) {
		slotKey := fmt.Sprintf("clear-%s-%s", now.Format("2006-01-02"), tower.Schedule.EndTime)
		s.mu.Lock()
		if s.lastTower != slotKey {
			s.lastTower = slotKey
			clearFn := s.executor.TowerClearFunc
			s.mu.Unlock()

			if clearFn != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = clearFn(ctx)
			}
		} else {
			s.mu.Unlock()
		}
	}
}

func (s *Scheduler) checkAutoUpdate(now time.Time, uptime float64) {
	upCfg := s.cfgMgr.Get().Update
	if upCfg.AutoUpdateEnabled == 0 || upCfg.AutoUpdateTime == "" {
		return
	}

	if !NowMatchesHHMM(upCfg.AutoUpdateTime, now, 10) {
		return
	}

	slotKey := fmt.Sprintf("%s-%s", now.Format("2006-01-02"), upCfg.AutoUpdateTime)
	s.mu.Lock()
	if s.lastUpdate == slotKey {
		s.mu.Unlock()
		return
	}
	s.lastUpdate = slotKey
	updateFn := s.executor.AutoUpdateFunc
	s.mu.Unlock()

	if updateFn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = updateFn(ctx)
	}
}
