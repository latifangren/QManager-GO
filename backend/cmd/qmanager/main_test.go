package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"qmanager/internal/api/router"
	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestAppMain_BootAndShutdown(t *testing.T) {
	// Setup temporary environment
	tempDir := t.TempDir()
	confPath := tempDir + "/qmanager.conf"
	os.Setenv("PORT", "18080")
	defer os.Unsetenv("PORT")

	cfgMgr, err := config.NewManager(confPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	mock := atengine.NewMockTransport()
	mock.SetResponse("AT", "OK")
	mock.SetResponse(`AT+QENG="servingcell"`, `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`)
	mock.SetResponse(`AT+QCAINFO`, `+QCAINFO: "PCC",1675,100,"LTE BAND 3",1,218,-85,-9,-62,18`)

	eng := atengine.NewEngine(mock)
	defer eng.Close()

	id := platform.Identity{
		Model:    "RG501QEU_VD",
		Revision: "RG501QEUAAR12A08M4G",
		Serial:   "61368cd2",
		SoC:      "SDX55",
	}

	poller := telemetry.NewPoller(eng, id, 1*time.Second)
	poller.Start()
	defer poller.Stop()

	prober := telemetry.NewPingProber("1.1.1.1:53", 1*time.Second)
	prober.Start()
	defer prober.Stop()

	watchdog := telemetry.NewWatchdog(eng, cfgMgr, prober)
	watchdog.Start()
	defer watchdog.Stop()

	smsForwarder := telemetry.NewSMSForwarder(eng, cfgMgr)
	smsForwarder.Start()
	defer smsForwarder.Stop()

	scheduler := telemetry.NewScheduler(eng, cfgMgr)
	scheduler.Start()
	defer scheduler.Stop()

	services := router.AppServices{
		Engine:    eng,
		Poller:    poller,
		Prober:    prober,
		Watchdog:  watchdog,
		ConfigMgr: cfgMgr,
		Identity:  id,
		DistFS:    distFS,
		ConfigDir: tempDir,
	}

	r := router.NewRouter(services)

	server := &http.Server{
		Addr:    ":18080",
		Handler: r,
	}

	go func() {
		_ = server.ListenAndServe()
	}()

	time.Sleep(100 * time.Millisecond)

	// Verify server responds
	resp, err := http.Get("http://localhost:18080/api/v1/status")
	if err != nil {
		t.Fatalf("failed to query test server: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown error: %v", err)
	}
}

func TestAppMain_FullFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- AppMain(ctx, "18082")
	}()

	time.Sleep(100 * time.Millisecond)
	cancel() // Trigger graceful shutdown via context cancellation

	select {
	case err := <-done:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("AppMain returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Errorf("AppMain did not shutdown in time")
	}

	// Test AppMain with flags and custom config dir
	tmpConfigDir := t.TempDir()
	ctxFlags, cancelFlags := context.WithCancel(context.Background())
	doneFlags := make(chan error, 1)

	go func() {
		doneFlags <- AppMain(ctxFlags, "", "-port", "18083", "-config-dir", tmpConfigDir, "-device", "mock")
	}()

	time.Sleep(100 * time.Millisecond)
	cancelFlags()

	select {
	case err := <-doneFlags:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("AppMain with flags returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Errorf("AppMain with flags did not shutdown in time")
	}
}
