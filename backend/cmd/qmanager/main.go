package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"qmanager/internal/api/router"
	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

//go:embed dist/*
var distFS embed.FS

// AppMain boots the embedded HTTP server and starts all background pollers and controllers.
func AppMain(ctx context.Context, port string, optionalFlags ...string) error {
	// Parse CLI flags and environment variables
	fs := flag.NewFlagSet("qmanager", flag.ContinueOnError)
	flagPort := fs.String("port", "", "HTTP server port (default 80, env: PORT / QM_PORT)")
	flagDevice := fs.String("device", "", "Modem AT device path / COM port (env: AT_DEVICE / QM_AT_DEVICE)")
	flagConfigDir := fs.String("config-dir", "", "Configuration directory (default /etc/qmanager, env: QM_CONFIG_DIR)")

	if len(optionalFlags) > 0 {
		_ = fs.Parse(optionalFlags)
	} else if len(os.Args) > 1 {
		_ = fs.Parse(os.Args[1:])
	}

	// 1. Resolve Port
	if port == "" {
		if *flagPort != "" {
			port = *flagPort
		} else if envPort := os.Getenv("PORT"); envPort != "" {
			port = envPort
		} else if envPort := os.Getenv("QM_PORT"); envPort != "" {
			port = envPort
		} else {
			port = "80"
		}
	}

	// 2. Resolve AT Device
	atDevice := *flagDevice
	if atDevice == "" {
		if envDev := os.Getenv("AT_DEVICE"); envDev != "" {
			atDevice = envDev
		} else if envDev := os.Getenv("QM_AT_DEVICE"); envDev != "" {
			atDevice = envDev
		}
	}

	// 3. Resolve Config Directory
	configDir := *flagConfigDir
	if configDir == "" {
		if envDir := os.Getenv("QM_CONFIG_DIR"); envDir != "" {
			configDir = envDir
		} else {
			configDir = "/etc/qmanager"
		}
	}

	fmt.Printf("🚀 Initializing QManager Go Engine (port=%s, at_device=%s, config_dir=%s)...\n", port, atDevice, configDir)

	// 1. Hardware & Platform Detection
	identity := platform.DetectIdentity("", "")
	fmt.Printf("📦 Detected Platform: Model=%s, SoC=%s, Serial=%s\n", identity.Model, identity.SoC, identity.Serial)
	_ = platform.InitFirewallRules()

	// 2. Configuration Store
	confFilePath := filepath.Join(configDir, "qmanager.conf")
	cfgMgr, err := config.NewManager(confFilePath)
	if err != nil {
		log.Printf("⚠️ Config manager init warning: %v\n", err)
	}

	// 3. AT Command Transport & Engine
	atTransport := atengine.AutoDetectTransport(atDevice)
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
		ConfigDir: configDir,
	}

	r := router.NewRouter(appServices)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("📡 QManager Backend listening on :%s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Println("🛑 Shutting down HTTP server gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := AppMain(ctx, ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Fatal error during execution: %v\n", err)
	}
}
