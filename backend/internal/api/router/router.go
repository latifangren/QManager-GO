package router

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"qmanager/internal/api/handlers"
	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

// AppServices bundles all backend dependencies.
type AppServices struct {
	Engine    *atengine.Engine
	Poller    *telemetry.Poller
	Prober    *telemetry.PingProber
	Watchdog  *telemetry.Watchdog
	ConfigMgr *config.Manager
	Identity  platform.Identity
	DistFS    embed.FS
}

// NewRouter constructs and mounts all API and static endpoints.
func NewRouter(s AppServices) http.Handler {
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	authH := handlers.NewAuthHandler("admin")
	bandFailoverH := handlers.NewBandFailoverHandler()
	cellH := handlers.NewCellularHandler(s.Engine, s.Poller, bandFailoverH)
	apnH := handlers.NewCellularApnHandler(s.Engine, s.ConfigMgr)
	imeiH := handlers.NewCellularImeiHandler(s.Engine, s.Poller, s.ConfigMgr)
	fplmnH := handlers.NewCellularFplmnHandler(s.Engine)
	priorityH := handlers.NewNetworkPriorityHandler(s.Engine)
	mbnH := handlers.NewCellularMbnHandler(s.Engine)
	freqH := handlers.NewFrequencyLockHandler(s.Engine)
	freqCalcH := handlers.NewFrequencyCalculatorHandler()
	towerH := handlers.NewTowerScheduleHandler(s.Engine)
	profileH := handlers.NewSIMProfileHandler(s.Engine)
	scenarioH := handlers.NewScenarioHandler(s.Engine)
	cellScanH := handlers.NewCellScannerHandler(s.Engine)
	neighbourH := handlers.NewNeighbourScannerHandler(s.Engine)
	speedtestH := handlers.NewSpeedtestHandler()
	netH := handlers.NewNetworkHandler(s.Prober)
	ethernetH := handlers.NewEthernetHandler()
	dataUsageH := handlers.NewDataUsageHandler()
	ipptH := handlers.NewIPPassthroughHandler(s.Engine)
	mtuH := handlers.NewNetworkMTUHandler()
	videoOptH := handlers.NewVideoOptimizerHandler()
	customDNSH := handlers.NewCustomDNSHandler()
	tailscaleH := handlers.NewTailscaleHandler()
	publicH := handlers.NewPublicHandler(s.Poller, s.ConfigMgr, s.Identity)
	watchdogH := handlers.NewWatchdogHandler(s.ConfigMgr, s.Watchdog)
	alertsH := handlers.NewAlertsHandler()
	simRegH := handlers.NewSimRegistryHandler()
	sysH := handlers.NewSystemHandler(s.Identity, s.ConfigMgr)
	smsH := handlers.NewSMSHandler(s.Engine)
	smsForwardH := handlers.NewSMSForwardingHandler(s.Engine, s.ConfigMgr)
	updateH := handlers.NewUpdateHandler(s.ConfigMgr)
	logsH := handlers.NewLogsHandler()
	langPacksH := handlers.NewLanguagePacksHandler()
	healthCheckH := handlers.NewHealthCheckHandler(s.Engine, s.Poller, s.Identity)
	historyH := handlers.NewHistoryHandler()

	// API Routes (v1)
	r.Route("/api/v1", func(api chi.Router) {
		// Public Auth & Overview
		api.Post("/auth/login", authH.Login)
		api.Post("/auth/setup", authH.Login)
		api.Get("/auth/check", authH.Check)
		api.Post("/auth/logout", authH.Logout)
		api.Get("/public/overview", publicH.Overview)
		api.Get("/public/hostname", publicH.Hostname)
		api.Get("/public/units", publicH.Units)

		// Public Telemetry & Status
		api.Get("/status", cellH.Status)
		api.Get("/system/info", sysH.Info)
		api.Get("/network/ping", netH.PingStats)

		// Telemetry History
		api.Get("/telemetry/history/signal", historyH.FetchSignalHistory)
		api.Get("/telemetry/history/ping", historyH.FetchPingHistory)
		api.Get("/telemetry/events", historyH.FetchEvents)

		// Protected Routes
		api.Group(func(prot chi.Router) {
			prot.Use(authH.Middleware)

			// Auth Password Management
			prot.Post("/auth/password", authH.ChangePassword)
			prot.Post("/auth/ssh_password", authH.ChangeSSHPassword)

			// Cellular / AT / Bands / Towers
			prot.Post("/at/send", cellH.SendCommand)
			prot.Get("/cellular/bands", cellH.GetBands)
			prot.Post("/cellular/bands/lock", cellH.LockBands)
			prot.Get("/cellular/bands/failover/status", bandFailoverH.Status)
			prot.Post("/cellular/bands/failover/toggle", bandFailoverH.Toggle)
			prot.Post("/cellular/tower/lock", cellH.LockTower)
			prot.Post("/cellular/tower/unlock", cellH.UnlockTower)
			prot.Get("/cellular/tower/status", towerH.Status)
			prot.Post("/cellular/tower/settings", towerH.Settings)
			prot.Post("/cellular/tower/schedule", towerH.Schedule)
			prot.Get("/cellular/tower/failover-status", towerH.FailoverStatus)

			// Frequency Locking & Calculation
			prot.Get("/cellular/frequency", freqH.Status)
			prot.Post("/cellular/frequency/lock", freqH.Lock)
			prot.Get("/cellular/frequency/calculate", freqCalcH.Calculate)

			// Cell & Neighbour Scanners
			prot.Post("/cellular/scanner/start", cellScanH.StartScan)
			prot.Get("/cellular/scanner/status", cellScanH.ScanStatus)
			prot.Post("/cellular/neighbour/start", neighbourH.StartScan)
			prot.Get("/cellular/neighbour/status", neighbourH.ScanStatus)

			// Diagnostics / Speedtest
			prot.Get("/diagnostics/speedtest/check", speedtestH.CheckAvailable)
			prot.Get("/diagnostics/speedtest/servers", speedtestH.ListServers)
			prot.Post("/diagnostics/speedtest/start", speedtestH.StartTest)
			prot.Get("/diagnostics/speedtest/status", speedtestH.GetStatus)
			prot.Post("/diagnostics/speedtest/stop", speedtestH.StopTest)

			// SIM Profiles
			prot.Get("/cellular/profiles", profileH.List)
			prot.Post("/cellular/profiles", profileH.Save)
			prot.Get("/cellular/profiles/current-settings", profileH.CurrentSettings)
			prot.Get("/cellular/profiles/apply-status", profileH.ApplyStatus)
			prot.Post("/cellular/profiles/apply", profileH.Apply)
			prot.Get("/cellular/profiles/{id}", profileH.Get)
			prot.Delete("/cellular/profiles/{id}", profileH.Delete)

			// Connection Scenarios
			prot.Get("/cellular/scenarios", scenarioH.List)
			prot.Post("/cellular/scenarios", scenarioH.Save)
			prot.Get("/cellular/scenarios/active", scenarioH.Active)
			prot.Post("/cellular/scenarios/activate", scenarioH.Activate)
			prot.Delete("/cellular/scenarios/{id}", scenarioH.Delete)

			// Cellular Settings & Identity Suite
			prot.Get("/cellular/apn", apnH.GetAPN)
			prot.Post("/cellular/apn", apnH.SaveAPN)
			prot.Get("/cellular/imei", imeiH.GetIMEI)
			prot.Post("/cellular/imei", imeiH.SaveIMEI)
			prot.Get("/cellular/fplmn", fplmnH.GetFPLMN)
			prot.Post("/cellular/fplmn/clear", fplmnH.ClearFPLMN)
			prot.Get("/cellular/priority", priorityH.GetPriority)
			prot.Post("/cellular/priority", priorityH.SetPriority)
			prot.Get("/cellular/mbn", mbnH.GetMBN)
			prot.Post("/cellular/mbn", mbnH.SaveMBN)

			// Network, Traffic & Ethernet
			prot.Post("/network/ttl", netH.SetTTL)
			prot.Get("/network/dns", customDNSH.HandleGet)
			prot.Post("/network/dns", customDNSH.HandlePost)
			prot.Get("/network/ethernet", ethernetH.HandleEthernet)
			prot.Get("/network/data-usage", dataUsageH.GetDataUsed)
			prot.Post("/network/data-usage/reset", dataUsageH.ResetDataUsed)
			prot.Get("/network/passthrough", ipptH.Status)
			prot.Post("/network/passthrough", ipptH.Apply)
			prot.Get("/network/mtu", mtuH.GetMTU)
			prot.Post("/network/mtu", mtuH.SetMTU)
			prot.Get("/network/traffic-engine", videoOptH.HandleGet)
			prot.Post("/network/traffic-engine", videoOptH.HandlePost)
			prot.Get("/network/video-optimizer", videoOptH.HandleGet)
			prot.Post("/network/video-optimizer", videoOptH.HandlePost)

			// VPN / Tailscale
			prot.Get("/vpn/tailscale", tailscaleH.HandleTailscale)
			prot.Post("/vpn/tailscale", tailscaleH.HandleTailscale)

			// Monitoring, Watchdog & Alerts
			prot.Get("/monitoring/watchdog", watchdogH.HandleWatchdog)
			prot.Post("/monitoring/watchdog", watchdogH.HandleWatchdog)
			prot.Get("/monitoring/alerts", alertsH.HandleAlerts)
			prot.Post("/monitoring/alerts", alertsH.HandleAlerts)

			// SMS
			prot.Get("/sms", smsH.ListSMS)
			prot.Post("/sms/send", smsH.SendSMS)
			prot.Delete("/sms", smsH.DeleteSMS)
			prot.Get("/cellular/sms", smsH.ListSMS)
			prot.Post("/cellular/sms", smsH.HandleSMSAction)
			prot.Get("/cellular/sms/inbox", smsH.GetSMSCenter)
			prot.Post("/cellular/sms/send", smsH.SendSMS)
			prot.Delete("/cellular/sms", smsH.DeleteSMS)
			prot.Get("/cellular/sms/forwarding", smsForwardH.GetSettings)
			prot.Post("/cellular/sms/forwarding", smsForwardH.HandleAction)

			// System, SIM Registry, Language Packs, Health Check, OTA & Logs
			prot.Get("/system/config", sysH.GetConfig)
			prot.Post("/system/config", sysH.SaveConfig)
			prot.Get("/system/sim-registry", simRegH.HandleRegistry)
			prot.Post("/system/sim-registry", simRegH.HandleRegistry)
			prot.Get("/system/language-packs/list", langPacksH.List)
			prot.Post("/system/language-packs/install", langPacksH.Install)
			prot.Get("/system/language-packs/install-status", langPacksH.InstallStatus)
			prot.Post("/system/language-packs/remove", langPacksH.Remove)
			prot.Post("/system/health-check/run", healthCheckH.Run)
			prot.Get("/system/health-check/status", healthCheckH.Status)
			prot.Get("/system/health-check/download", healthCheckH.Download)
			prot.Post("/system/health-check/clear", healthCheckH.Clear)
			prot.Post("/system/reboot", sysH.Reboot)
			prot.Get("/system/update", updateH.CheckUpdate)
			prot.Post("/system/update", updateH.HandleUpdateAction)
			prot.Get("/system/logs", logsH.GetLogs)
			prot.Post("/system/logs", logsH.HandleLogsAction)
			prot.Get("/system/modem-subsys", logsH.ModemSubsys)
		})
	})

	// CGI Backwards-compatibility shim for older hooks
	r.Route("/cgi-bin/quecmanager", func(cgi chi.Router) {
		// Telemetry & AT
		cgi.Get("/at_cmd/fetch_data.sh", cellH.Status)
		cgi.Post("/at_cmd/send_command.sh", cellH.SendCommand)
		cgi.Get("/at_cmd/fetch_signal_history.sh", historyH.FetchSignalHistory)
		cgi.Get("/at_cmd/fetch_ping_history.sh", historyH.FetchPingHistory)
		cgi.Get("/at_cmd/fetch_events.sh", historyH.FetchEvents)

		// Auth
		cgi.Get("/auth/check.sh", authH.Check)
		cgi.Post("/auth/login.sh", authH.Login)
		cgi.Post("/auth/setup.sh", authH.Login)
		cgi.Post("/auth/logout.sh", authH.Logout)
		cgi.Post("/auth/password.sh", authH.ChangePassword)
		cgi.Post("/auth/ssh_password.sh", authH.ChangeSSHPassword)

		// Public Overview
		cgi.Get("/public/overview.sh", publicH.Overview)
		cgi.Get("/public/hostname.sh", publicH.Hostname)
		cgi.Get("/public/units.sh", publicH.Units)

		// Bands & Failover
		cgi.Get("/bands/current.sh", cellH.GetBands)
		cgi.Post("/bands/lock.sh", cellH.LockBands)
		cgi.Get("/bands/failover_status.sh", bandFailoverH.Status)
		cgi.Post("/bands/failover_toggle.sh", bandFailoverH.Toggle)

		// Scanner & Diagnostics CGI
		cgi.Post("/at_cmd/cell_scan_start.sh", cellScanH.StartScan)
		cgi.Get("/at_cmd/cell_scan_status.sh", cellScanH.ScanStatus)
		cgi.Post("/at_cmd/neighbour_scan_start.sh", neighbourH.StartScan)
		cgi.Get("/at_cmd/neighbour_scan_status.sh", neighbourH.ScanStatus)
		cgi.Get("/at_cmd/speedtest_check.sh", speedtestH.CheckAvailable)
		cgi.Get("/at_cmd/speedtest_servers.sh", speedtestH.ListServers)
		cgi.Post("/at_cmd/speedtest_start.sh", speedtestH.StartTest)
		cgi.Get("/at_cmd/speedtest_status.sh", speedtestH.GetStatus)

		// Frequency CGI
		cgi.Get("/frequency/status.sh", freqH.Status)
		cgi.Post("/frequency/lock.sh", freqH.Lock)

		// Tower CGI
		cgi.Get("/tower/status.sh", towerH.Status)
		cgi.Post("/tower/settings.sh", towerH.Settings)
		cgi.Post("/tower/schedule.sh", towerH.Schedule)
		cgi.Get("/tower/failover_status.sh", towerH.FailoverStatus)

		// Profiles CGI
		cgi.Get("/profiles/list.sh", profileH.List)
		cgi.Get("/profiles/get.sh", profileH.Get)
		cgi.Post("/profiles/save.sh", profileH.Save)
		cgi.Post("/profiles/delete.sh", profileH.Delete)
		cgi.Post("/profiles/apply.sh", profileH.Apply)
		cgi.Get("/profiles/apply_status.sh", profileH.ApplyStatus)
		cgi.Get("/profiles/current_settings.sh", profileH.CurrentSettings)

		// Scenarios CGI
		cgi.Get("/scenarios/list.sh", scenarioH.List)
		cgi.Get("/scenarios/active.sh", scenarioH.Active)
		cgi.Post("/scenarios/save.sh", scenarioH.Save)
		cgi.Post("/scenarios/activate.sh", scenarioH.Activate)
		cgi.Post("/scenarios/delete.sh", scenarioH.Delete)

		// Cellular settings CGI endpoints
		cgi.Get("/cellular/apn.sh", apnH.GetAPN)
		cgi.Post("/cellular/apn.sh", apnH.SaveAPN)
		cgi.Get("/cellular/imei.sh", imeiH.GetIMEI)
		cgi.Post("/cellular/imei.sh", imeiH.SaveIMEI)
		cgi.Get("/cellular/fplmn.sh", fplmnH.GetFPLMN)
		cgi.Post("/cellular/fplmn.sh", fplmnH.ClearFPLMN)
		cgi.Get("/cellular/network_priority.sh", priorityH.GetPriority)
		cgi.Post("/cellular/network_priority.sh", priorityH.SetPriority)
		cgi.Get("/cellular/mbn.sh", mbnH.GetMBN)
		cgi.Post("/cellular/mbn.sh", mbnH.SaveMBN)

		// Network & Ethernet CGI endpoints
		cgi.Get("/network/ip_passthrough.sh", ipptH.Status)
		cgi.Post("/network/ip_passthrough.sh", ipptH.Apply)
		cgi.Get("/network/mtu.sh", mtuH.GetMTU)
		cgi.Post("/network/mtu.sh", mtuH.SetMTU)
		cgi.Get("/network/ethernet.sh", ethernetH.HandleEthernet)
		cgi.Get("/network/data_used.sh", dataUsageH.GetDataUsed)
		cgi.Post("/network/data_used_reset.sh", dataUsageH.ResetDataUsed)
		cgi.Get("/network/video_optimizer.sh", videoOptH.HandleGet)
		cgi.Post("/network/video_optimizer.sh", videoOptH.HandlePost)
		cgi.Get("/network/custom_dns.sh", customDNSH.HandleGet)
		cgi.Post("/network/custom_dns.sh", customDNSH.HandlePost)

		// VPN Tailscale CGI endpoints
		cgi.Get("/vpn/tailscale.sh", tailscaleH.HandleTailscale)
		cgi.Post("/vpn/tailscale.sh", tailscaleH.HandleTailscale)

		// Monitoring CGI endpoints
		cgi.Get("/monitoring/watchdog.sh", watchdogH.HandleWatchdog)
		cgi.Post("/monitoring/watchdog.sh", watchdogH.HandleWatchdog)
		cgi.Get("/monitoring/alerts.sh", alertsH.HandleAlerts)
		cgi.Post("/monitoring/alerts.sh", alertsH.HandleAlerts)

		// SMS Center & Forwarding CGI endpoints
		cgi.Get("/cellular/sms.sh", smsH.GetSMSCenter)
		cgi.Post("/cellular/sms.sh", smsH.HandleSMSAction)
		cgi.Get("/cellular/sms_forwarding.sh", smsForwardH.GetSettings)
		cgi.Post("/cellular/sms_forwarding.sh", smsForwardH.HandleAction)

		// Language Packs CGI endpoints
		cgi.Get("/system/language-packs/list.sh", langPacksH.List)
		cgi.Post("/system/language-packs/install.sh", langPacksH.Install)
		cgi.Get("/system/language-packs/install_status.sh", langPacksH.InstallStatus)
		cgi.Post("/system/language-packs/remove.sh", langPacksH.Remove)

		// Health Check CGI endpoints
		cgi.Post("/system/health-check/run.sh", healthCheckH.Run)
		cgi.Get("/system/health-check/status.sh", healthCheckH.Status)
		cgi.Get("/system/health-check/download.sh", healthCheckH.Download)
		cgi.Post("/system/health-check/clear.sh", healthCheckH.Clear)

		// System Update, Logs, Settings & Subsystem CGI endpoints
		cgi.Get("/device/about.sh", sysH.Info)
		cgi.Get("/system/settings.sh", sysH.GetConfig)
		cgi.Post("/system/settings.sh", sysH.SaveConfig)
		cgi.Get("/system/sim_registry.sh", simRegH.HandleRegistry)
		cgi.Post("/system/sim_registry.sh", simRegH.HandleRegistry)
		cgi.Post("/system/reboot.sh", sysH.Reboot)
		cgi.Get("/system/update.sh", updateH.CheckUpdate)
		cgi.Post("/system/update.sh", updateH.HandleUpdateAction)
		cgi.Get("/system/logs.sh", logsH.GetLogs)
		cgi.Post("/system/logs.sh", logsH.HandleLogsAction)
		cgi.Get("/system/modem-subsys.sh", logsH.ModemSubsys)
	})

	// Embedded Static Frontend
	staticContent, err := fs.Sub(s.DistFS, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(staticContent))
		r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			fileServer.ServeHTTP(w, req)
		}))
	}

	return r
}
