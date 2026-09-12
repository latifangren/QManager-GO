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
	"qmanager/internal/dpi"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
	"qmanager/internal/telemetry/bandwidth"
	"qmanager/internal/tlsgen"
	"qmanager/internal/sshd"
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
	defer engine.Close()

	// Baseband Readiness Probe / Warmup
	probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer probeCancel()
	if err := engine.WaitReady(probeCtx, 10, 500*time.Millisecond); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("⚠️ Modem readiness probe warning: %v (continuing startup)...\n", err)
	} else {
		fmt.Println("📡 Modem baseband ready. Starting telemetry poller & watchdog...")
	}

	if ctx.Err() != nil {
		return nil
	}

	// 4. Background Telemetry & Probers
	poller := telemetry.NewPoller(engine, identity, 1*time.Second)

	prober := telemetry.NewPingProber("1.1.1.1:53", 2*time.Second)
	prober.Start()
	defer prober.Stop()

	poller.SetProber(prober)
	poller.Start()
	defer poller.Stop()

	watchdog := telemetry.NewWatchdog(engine, cfgMgr, prober)
	watchdog.Start()
	defer watchdog.Stop()

	smsForwarder := telemetry.NewSMSForwarder(engine, cfgMgr)
	smsForwarder.Start()
	defer smsForwarder.Stop()

	scheduler := telemetry.NewScheduler(engine, cfgMgr)
	scheduler.Start()
	defer scheduler.Stop()

	// 5. Bandwidth Monitoring & vnStat Collector
	bwCollector := bandwidth.NewCollector(filepath.Join(configDir, "bandwidth.json"))
	bwCollector.Start()
	defer func() {
		_ = bwCollector.Close()
	}()

	// 6. DPI & Traffic Engine Lifecycle
	dpi.SyncState()
	defer dpi.GetManager().StopEngine()

	// 7. Native Go Standalone SSH Server Daemon
	sshManager := sshd.NewManager(cfgMgr, filepath.Join(configDir, "ssh"))
	if err := sshManager.Start(); err != nil {
		log.Printf("⚠️ Native SSH server start failed: %v\n", err)
	}
	defer func() {
		_ = sshManager.Stop()
	}()

	// 8. Router & Server
	appServices := router.AppServices{
		Engine:     engine,
		Poller:     poller,
		Prober:     prober,
		Watchdog:   watchdog,
		ConfigMgr:  cfgMgr,
		Bandwidth:  bwCollector,
		Identity:   identity,
		DistFS:     distFS,
		ConfigDir:  configDir,
		SSHManager: sshManager,
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

	// Optional HTTPS server with auto-generated/existing TLS certificates
	tlsCert := filepath.Join(configDir, "certs/server.crt")
	tlsKey := filepath.Join(configDir, "certs/server.key")
	if _, err := os.Stat(tlsCert); err != nil {
		tlsCert = "/usrdata/qmanager/certs/server.crt"
		tlsKey = "/usrdata/qmanager/certs/server.key"
	}

	// Auto-generate self-signed certs if missing
	if _, err := os.Stat(tlsCert); err != nil {
		genCert, genKey, errGen := tlsgen.EnsureCertificates(filepath.Join(configDir, "tls"))
		if errGen == nil {
			tlsCert = genCert
			tlsKey = genKey
		} else {
			log.Printf("⚠️ TLS cert generation failed: %v\n", errGen)
		}
	}

	var httpsServer *http.Server
	if _, err := os.Stat(tlsCert); err == nil {
		if _, err := os.Stat(tlsKey); err == nil {
			httpsServer = &http.Server{
				Addr:         ":443",
				Handler:      r,
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 60 * time.Second,
			}
			go func() {
				log.Println("🔒 QManager Backend listening on :443 (HTTPS)")
				if err := httpsServer.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
					log.Printf("⚠️ HTTPS server error: %v\n", err)
				}
			}()
		}
	}

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Println("🛑 Shutting down server gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if httpsServer != nil {
			_ = httpsServer.Shutdown(shutdownCtx)
		}
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
