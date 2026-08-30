package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"qmanager/internal/api/router"
	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

//go:embed all:dist
var distFS embed.FS

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("🚀 Initializing QManager Go Single-Binary Engine...")

	// 1. Hardware & Platform Detection
	identity := platform.DetectIdentity("", "")
	fmt.Printf("📦 Detected Platform: Model=%s, SoC=%s, Serial=%s\n", identity.Model, identity.SoC, identity.Serial)

	// 2. Configuration Store
	cfgMgr, err := config.NewManager("")
	if err != nil {
		log.Printf("⚠️ Config manager init warning: %v\n", err)
	}

	// 3. AT Command Transport & Engine
	atTransport := atengine.AutoDetectTransport()
	defer atTransport.Close()
	engine := atengine.NewEngine(atTransport)

	// 4. Background Telemetry & Probers
	poller := telemetry.NewPoller(engine, identity, 1*time.Second)
	poller.Start()
	defer poller.Stop()

	prober := telemetry.NewPingProber("1.1.1.1:53", 2*time.Second)
	prober.Start()
	defer prober.Stop()

	watchdog := telemetry.NewWatchdog(engine, cfgMgr, prober)
	watchdog.Start()
	defer watchdog.Stop()

	smsForwarder := telemetry.NewSMSForwarder(engine, cfgMgr)
	smsForwarder.Start()
	defer smsForwarder.Stop()

	scheduler := telemetry.NewScheduler(engine, cfgMgr)
	scheduler.Start()
	defer scheduler.Stop()

	// 5. Router & Server
	appServices := router.AppServices{
		Engine:    engine,
		Poller:    poller,
		Prober:    prober,
		Watchdog:  watchdog,
		ConfigMgr: cfgMgr,
		Identity:  identity,
		DistFS:    distFS,
	}

	r := router.NewRouter(appServices)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	// Graceful shutdown handling
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\n🛑 Shutting down QManager daemon...")
		_ = server.Close()
	}()

	fmt.Printf("🌐 QManager HTTP API & Web UI listening on :%s\n", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("❌ Server listen failed: %v\n", err)
	}
}
