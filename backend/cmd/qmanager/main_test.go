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

func TestAppServicesInitialization(t *testing.T) {
	mock := atengine.NewMockTransport()
	engine := atengine.NewEngine(mock)
	defer engine.Close()

	identity := platform.Identity{
		Model:   "RG501Q-EU",
		Revision: "RG501QEUAAR12A08M4G",
		SoC:     "SDX55",
		IsSDX55: true,
	}

	cfgMgr, err := config.NewManager(t.TempDir() + "/qmanager.conf")
	if err != nil {
		t.Fatalf("config manager error: %v", err)
	}

	poller := telemetry.NewPoller(engine, identity, 100*time.Millisecond)
	poller.Start()
	defer poller.Stop()

	prober := telemetry.NewPingProber("127.0.0.1:9", 50*time.Millisecond)
	prober.Start()
	defer prober.Stop()

	watchdog := telemetry.NewWatchdog(engine, cfgMgr, prober)
	watchdog.Start()
	defer watchdog.Stop()

	services := router.AppServices{
		Engine:    engine,
		Poller:    poller,
		Prober:    prober,
		Watchdog:  watchdog,
		ConfigMgr: cfgMgr,
		Identity:  identity,
		DistFS:    distFS,
	}

	r := router.NewRouter(services)
	if r == nil {
		t.Fatalf("expected non-nil router instance")
	}

	// Test server startup and graceful shutdown
	server := &http.Server{
		Addr:    "127.0.0.1:0", // random available port
		Handler: r,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.ListenAndServe()
	}()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown failed: %v", err)
	}

	<-done
}

func TestAppMain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- AppMain(ctx, "0") // port 0 assigns an ephemeral port
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
}

func TestEnvironmentPortFallback(t *testing.T) {
	t.Setenv("PORT", "9999")
	port := os.Getenv("PORT")
	if port != "9999" {
		t.Errorf("expected PORT=9999, got %s", port)
	}
}

func TestMain_Execution(t *testing.T) {
	called := false
	orig := mainRunner
	defer func() { mainRunner = orig }()

	mainRunner = func() {
		called = true
	}

	main()
	if !called {
		t.Errorf("main() did not call mainRunner")
	}
}
