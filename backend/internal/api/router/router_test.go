package router

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

//go:embed *
var testFS embed.FS

func TestRouter_MountsAndEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	confPath := filepath.Join(tempDir, "qmanager.conf")
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
	watchdog := telemetry.NewWatchdog(eng, cfgMgr, prober)

	services := AppServices{
		Engine:     eng,
		Poller:     poller,
		Prober:     prober,
		Watchdog:   watchdog,
		ConfigMgr:  cfgMgr,
		Identity:   id,
		DistFS:     testFS,
		ConfigDir:  tempDir,
		LocalesDir: filepath.Join(tempDir, "locales-packs"),
		CommandRunner: func(name string, arg ...string) error {
			return nil // Mock command runner to avoid iptables permission errors in CI
		},
	}

	handler := NewRouter(services)

	// 1. Test Public Overview
	reqPub := httptest.NewRequest("GET", "/api/v1/public/overview", nil)
	wPub := httptest.NewRecorder()
	handler.ServeHTTP(wPub, reqPub)
	if wPub.Code != http.StatusOK {
		t.Errorf("expected status 200 for public/overview, got %d", wPub.Code)
	}

	reqPubCgi := httptest.NewRequest("GET", "/cgi-bin/quecmanager/public/overview.sh", nil)
	wPubCgi := httptest.NewRecorder()
	handler.ServeHTTP(wPubCgi, reqPubCgi)
	if wPubCgi.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI public/overview.sh, got %d", wPubCgi.Code)
	}

	// 2. Test Hostname & Units
	reqHn := httptest.NewRequest("GET", "/cgi-bin/quecmanager/public/hostname.sh", nil)
	wHn := httptest.NewRecorder()
	handler.ServeHTTP(wHn, reqHn)
	if wHn.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI public/hostname.sh, got %d", wHn.Code)
	}

	reqUnits := httptest.NewRequest("GET", "/cgi-bin/quecmanager/public/units.sh", nil)
	wUnits := httptest.NewRecorder()
	handler.ServeHTTP(wUnits, reqUnits)
	if wUnits.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI public/units.sh, got %d", wUnits.Code)
	}

	// 3. Test Band Failover Status & Toggle
	reqBandFo := httptest.NewRequest("GET", "/cgi-bin/quecmanager/bands/failover_status.sh", nil)
	wBandFo := httptest.NewRecorder()
	handler.ServeHTTP(wBandFo, reqBandFo)
	if wBandFo.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI bands/failover_status.sh, got %d", wBandFo.Code)
	}

	reqBandFoToggle := httptest.NewRequest("POST", "/cgi-bin/quecmanager/bands/failover_toggle.sh", strings.NewReader(`{"enabled":true}`))
	wBandFoToggle := httptest.NewRecorder()
	handler.ServeHTTP(wBandFoToggle, reqBandFoToggle)
	if wBandFoToggle.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI bands/failover_toggle.sh, got %d", wBandFoToggle.Code)
	}

	// 4. Test Monitoring Watchdog & Alerts
	reqWd := httptest.NewRequest("GET", "/cgi-bin/quecmanager/monitoring/watchdog.sh", nil)
	wWd := httptest.NewRecorder()
	handler.ServeHTTP(wWd, reqWd)
	if wWd.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI monitoring/watchdog.sh, got %d", wWd.Code)
	}

	reqAlerts := httptest.NewRequest("GET", "/cgi-bin/quecmanager/monitoring/alerts.sh", nil)
	wAlerts := httptest.NewRecorder()
	handler.ServeHTTP(wAlerts, reqAlerts)
	if wAlerts.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI monitoring/alerts.sh, got %d", wAlerts.Code)
	}

	// 5. Test Network Ethernet & Data Usage
	reqEth := httptest.NewRequest("GET", "/cgi-bin/quecmanager/network/ethernet.sh", nil)
	wEth := httptest.NewRecorder()
	handler.ServeHTTP(wEth, reqEth)
	if wEth.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI network/ethernet.sh, got %d", wEth.Code)
	}

	reqDataUsed := httptest.NewRequest("GET", "/cgi-bin/quecmanager/network/data_used.sh", nil)
	wDataUsed := httptest.NewRecorder()
	handler.ServeHTTP(wDataUsed, reqDataUsed)
	if wDataUsed.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI network/data_used.sh, got %d", wDataUsed.Code)
	}

	// 6. Test VPN Tailscale
	reqTs := httptest.NewRequest("GET", "/cgi-bin/quecmanager/vpn/tailscale.sh", nil)
	wTs := httptest.NewRecorder()
	handler.ServeHTTP(wTs, reqTs)
	if wTs.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI vpn/tailscale.sh, got %d", wTs.Code)
	}

	// 7. Test System Settings & SIM Registry
	reqSettings := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/settings.sh", nil)
	wSettings := httptest.NewRecorder()
	handler.ServeHTTP(wSettings, reqSettings)
	if wSettings.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI system/settings.sh, got %d", wSettings.Code)
	}

	reqSimReg := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/sim_registry.sh", nil)
	wSimReg := httptest.NewRecorder()
	handler.ServeHTTP(wSimReg, reqSimReg)
	if wSimReg.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI system/sim_registry.sh, got %d", wSimReg.Code)
	}

	// 8. Test Auth Logout
	reqLogout := httptest.NewRequest("POST", "/cgi-bin/quecmanager/auth/logout.sh", nil)
	wLogout := httptest.NewRecorder()
	handler.ServeHTTP(wLogout, reqLogout)
	if wLogout.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI auth/logout.sh, got %d", wLogout.Code)
	}

	// 9. Test Language Packs
	reqLangList := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/language-packs/list.sh", nil)
	wLangList := httptest.NewRecorder()
	handler.ServeHTTP(wLangList, reqLangList)
	if wLangList.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI language-packs/list.sh, got %d", wLangList.Code)
	}

	reqLangInstall := httptest.NewRequest("POST", "/cgi-bin/quecmanager/system/language-packs/install.sh", strings.NewReader(`{"code":"id"}`))
	wLangInstall := httptest.NewRecorder()
	handler.ServeHTTP(wLangInstall, reqLangInstall)
	if wLangInstall.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI language-packs/install.sh, got %d", wLangInstall.Code)
	}

	// 10. Test Health Check
	reqHcRun := httptest.NewRequest("POST", "/cgi-bin/quecmanager/system/health-check/run.sh", nil)
	wHcRun := httptest.NewRecorder()
	handler.ServeHTTP(wHcRun, reqHcRun)
	if wHcRun.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI health-check/run.sh, got %d", wHcRun.Code)
	}

	reqHcStatus := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/health-check/status.sh", nil)
	wHcStatus := httptest.NewRecorder()
	handler.ServeHTTP(wHcStatus, reqHcStatus)
	if wHcStatus.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI health-check/status.sh, got %d", wHcStatus.Code)
	}

	// 11. Test Telemetry History (Signal, Ping, Events)
	reqSigHist := httptest.NewRequest("GET", "/cgi-bin/quecmanager/at_cmd/fetch_signal_history.sh", nil)
	wSigHist := httptest.NewRecorder()
	handler.ServeHTTP(wSigHist, reqSigHist)
	if wSigHist.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI fetch_signal_history.sh, got %d", wSigHist.Code)
	}

	reqPingHist := httptest.NewRequest("GET", "/cgi-bin/quecmanager/at_cmd/fetch_ping_history.sh", nil)
	wPingHist := httptest.NewRecorder()
	handler.ServeHTTP(wPingHist, reqPingHist)
	if wPingHist.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI fetch_ping_history.sh, got %d", wPingHist.Code)
	}

	reqEvents := httptest.NewRequest("GET", "/cgi-bin/quecmanager/at_cmd/fetch_events.sh", nil)
	wEvents := httptest.NewRecorder()
	handler.ServeHTTP(wEvents, reqEvents)
	if wEvents.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI fetch_events.sh, got %d", wEvents.Code)
	}

	// 12. Test Auth Middleware on Protected Routes
	// Unauthorized request (no token)
	reqProtNoAuth := httptest.NewRequest("GET", "/api/v1/cellular/bands", nil)
	wProtNoAuth := httptest.NewRecorder()
	handler.ServeHTTP(wProtNoAuth, reqProtNoAuth)
	if wProtNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for unauthenticated protected route, got %d", wProtNoAuth.Code)
	}

	// Login/Setup to obtain valid token (length >= 6 when setup_required)
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"password":"admin123","confirm":"admin123"}`))
	wLogin := httptest.NewRecorder()
	handler.ServeHTTP(wLogin, reqLogin)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("expected status 200 for login, got %d", wLogin.Code)
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(wLogin.Body).Decode(&loginResp)
	if loginResp.Token == "" {
		t.Fatalf("login did not return token")
	}

	// Authenticated request with Bearer header
	mock.SetResponse(`AT+QNWPREFCFG="lte_band";+QNWPREFCFG="nsa_nr5g_band";+QNWPREFCFG="nr5g_band"`, `+QNWPREFCFG: "lte_band",1:3:5:8:40`+"\r\n"+`+QNWPREFCFG: "nsa_nr5g_band",1:3:41:78`+"\r\n"+`+QNWPREFCFG: "nr5g_band",1:3:41:78`+"\r\nOK")
	reqProtAuth := httptest.NewRequest("GET", "/api/v1/cellular/bands", nil)
	reqProtAuth.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wProtAuth := httptest.NewRecorder()
	handler.ServeHTTP(wProtAuth, reqProtAuth)
	if wProtAuth.Code != http.StatusOK {
		t.Errorf("expected status 200 for authenticated protected route with Bearer token, got %d: %s", wProtAuth.Code, wProtAuth.Body.String())
	}

	// Authenticated request with Cookie
	reqProtCookie := httptest.NewRequest("GET", "/api/v1/cellular/bands", nil)
	reqProtCookie.AddCookie(&http.Cookie{Name: "qm_auth_token", Value: loginResp.Token})
	wProtCookie := httptest.NewRecorder()
	handler.ServeHTTP(wProtCookie, reqProtCookie)
	if wProtCookie.Code != http.StatusOK {
		t.Errorf("expected status 200 for authenticated protected route with cookie, got %d: %s", wProtCookie.Code, wProtCookie.Body.String())
	}

	// Public routes bypass check
	reqStatus := httptest.NewRequest("GET", "/api/v1/status", nil)
	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Errorf("expected status 200 for public /status, got %d", wStatus.Code)
	}

	reqAuthCheck := httptest.NewRequest("GET", "/api/v1/auth/check", nil)
	wAuthCheck := httptest.NewRecorder()
	handler.ServeHTTP(wAuthCheck, reqAuthCheck)
	if wAuthCheck.Code != http.StatusUnauthorized { // Auth check returns 401 JSON when not logged in, but is handled by authH.Check, not blocked by middleware
		t.Errorf("expected status 401 for unauthenticated /auth/check, got %d", wAuthCheck.Code)
	}

	// 13. Test newly mounted CGI & REST settings endpoints
	reqQT := httptest.NewRequest("GET", "/cgi-bin/quecmanager/settings/quality_thresholds.sh", nil)
	wQT := httptest.NewRecorder()
	handler.ServeHTTP(wQT, reqQT)
	if wQT.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI settings/quality_thresholds.sh, got %d", wQT.Code)
	}

	reqPP := httptest.NewRequest("GET", "/cgi-bin/quecmanager/settings/ping_profile.sh", nil)
	wPP := httptest.NewRecorder()
	handler.ServeHTTP(wPP, reqPP)
	if wPP.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI settings/ping_profile.sh, got %d", wPP.Code)
	}

	reqDeact := httptest.NewRequest("POST", "/cgi-bin/quecmanager/profiles/deactivate.sh", nil)
	wDeact := httptest.NewRecorder()
	handler.ServeHTTP(wDeact, reqDeact)
	if wDeact.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI profiles/deactivate.sh, got %d", wDeact.Code)
	}

	reqKnownSims := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/known_sims.sh", nil)
	wKnownSims := httptest.NewRecorder()
	handler.ServeHTTP(wKnownSims, reqKnownSims)
	if wKnownSims.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI system/known_sims.sh, got %d", wKnownSims.Code)
	}

	// 14. Test DownloadLogs REST and CGI parity
	reqLogsDlCgi := httptest.NewRequest("GET", "/cgi-bin/quecmanager/system/logs_download.sh", nil)
	wLogsDlCgi := httptest.NewRecorder()
	handler.ServeHTTP(wLogsDlCgi, reqLogsDlCgi)
	if wLogsDlCgi.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI system/logs_download.sh, got %d", wLogsDlCgi.Code)
	}

	reqLogsDlRest := httptest.NewRequest("GET", "/api/v1/system/logs/download", nil)
	reqLogsDlRest.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wLogsDlRest := httptest.NewRecorder()
	handler.ServeHTTP(wLogsDlRest, reqLogsDlRest)
	if wLogsDlRest.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST system/logs/download, got %d", wLogsDlRest.Code)
	}

	// 15. Test Telemetry Stream REST and CGI routes
	ctxStream, cancelStream := context.WithCancel(context.Background())
	cancelStream()

	reqStreamRest := httptest.NewRequest("GET", "/api/v1/telemetry/stream", nil).WithContext(ctxStream)
	wStreamRest := httptest.NewRecorder()
	handler.ServeHTTP(wStreamRest, reqStreamRest)
	if wStreamRest.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream for /api/v1/telemetry/stream, got %s", wStreamRest.Header().Get("Content-Type"))
	}

	reqStreamCgi := httptest.NewRequest("GET", "/cgi-bin/quecmanager/api/stream/status", nil).WithContext(ctxStream)
	wStreamCgi := httptest.NewRecorder()
	handler.ServeHTTP(wStreamCgi, reqStreamCgi)
	if wStreamCgi.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream for CGI /api/stream/status, got %s", wStreamCgi.Header().Get("Content-Type"))
	}

	reqStreamCgi2 := httptest.NewRequest("GET", "/cgi-bin/quecmanager/telemetry_stream.sh", nil).WithContext(ctxStream)
	wStreamCgi2 := httptest.NewRecorder()
	handler.ServeHTTP(wStreamCgi2, reqStreamCgi2)
	if wStreamCgi2.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream for CGI /telemetry_stream.sh, got %s", wStreamCgi2.Header().Get("Content-Type"))
	}

	// 16. Test Bandwidth REST and CGI endpoints
	reqBwCgi := httptest.NewRequest("GET", "/cgi-bin/quecmanager/monitoring/bandwidth.sh", nil)
	wBwCgi := httptest.NewRecorder()
	handler.ServeHTTP(wBwCgi, reqBwCgi)
	if wBwCgi.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI /monitoring/bandwidth.sh, got %d", wBwCgi.Code)
	}

	reqBwRest := httptest.NewRequest("GET", "/api/v1/monitoring/bandwidth", nil)
	reqBwRest.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wBwRest := httptest.NewRecorder()
	handler.ServeHTTP(wBwRest, reqBwRest)
	if wBwRest.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST /monitoring/bandwidth, got %d", wBwRest.Code)
	}

	reqBwResetCgi := httptest.NewRequest("POST", "/cgi-bin/quecmanager/monitoring/bandwidth_reset.sh", strings.NewReader(`{"interface":"rmnet_data0"}`))
	wBwResetCgi := httptest.NewRecorder()
	handler.ServeHTTP(wBwResetCgi, reqBwResetCgi)
	if wBwResetCgi.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI /monitoring/bandwidth_reset.sh, got %d", wBwResetCgi.Code)
	}

	// Test POST /system/known_sims.sh does not return 405 Method Not Allowed
	reqKnownSimsPost := httptest.NewRequest("POST", "/cgi-bin/quecmanager/system/known_sims.sh", strings.NewReader(`{"iccid":"89860401102290123456","label":"Primary"}`))
	wKnownSimsPost := httptest.NewRecorder()
	handler.ServeHTTP(wKnownSimsPost, reqKnownSimsPost)
	if wKnownSimsPost.Code != http.StatusOK {
		t.Errorf("expected status 200 for POST CGI system/known_sims.sh, got %d", wKnownSimsPost.Code)
	}

	// Test GET & POST /network/ttl.sh
	reqTTLGet := httptest.NewRequest("GET", "/cgi-bin/quecmanager/network/ttl.sh", nil)
	wTTLGet := httptest.NewRecorder()
	handler.ServeHTTP(wTTLGet, reqTTLGet)
	if wTTLGet.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI network/ttl.sh GET, got %d", wTTLGet.Code)
	}

	reqTTLPost := httptest.NewRequest("POST", "/cgi-bin/quecmanager/network/ttl.sh", strings.NewReader(`{"ttl":64,"hl":64}`))
	wTTLPost := httptest.NewRecorder()
	handler.ServeHTTP(wTTLPost, reqTTLPost)
	if wTTLPost.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI network/ttl.sh POST, got %d", wTTLPost.Code)
	}

	// Test GET & POST /cellular/settings.sh
	reqCellSettingsGet := httptest.NewRequest("GET", "/cgi-bin/quecmanager/cellular/settings.sh", nil)
	wCellSettingsGet := httptest.NewRecorder()
	handler.ServeHTTP(wCellSettingsGet, reqCellSettingsGet)
	if wCellSettingsGet.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI cellular/settings.sh GET, got %d", wCellSettingsGet.Code)
	}

	reqCellSettingsPost := httptest.NewRequest("POST", "/cgi-bin/quecmanager/cellular/settings.sh", strings.NewReader(`{"mode_pref":"AUTO"}`))
	wCellSettingsPost := httptest.NewRecorder()
	handler.ServeHTTP(wCellSettingsPost, reqCellSettingsPost)
	if wCellSettingsPost.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI cellular/settings.sh POST, got %d", wCellSettingsPost.Code)
	}

	// Test POST /tower/lock.sh
	mock.SetResponse(`AT+QNWLOCK="common/4g",0`, "OK")
	reqTowerLockPost := httptest.NewRequest("POST", "/cgi-bin/quecmanager/tower/lock.sh", strings.NewReader(`{"type":"lte","action":"unlock"}`))
	wTowerLockPost := httptest.NewRecorder()
	handler.ServeHTTP(wTowerLockPost, reqTowerLockPost)
	if wTowerLockPost.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI tower/lock.sh POST, got %d", wTowerLockPost.Code)
	}

	// Test REST endpoints with Auth
	reqRESTSettings := httptest.NewRequest("GET", "/api/v1/cellular/settings", nil)
	reqRESTSettings.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wRESTSettings := httptest.NewRecorder()
	handler.ServeHTTP(wRESTSettings, reqRESTSettings)
	if wRESTSettings.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST /cellular/settings, got %d", wRESTSettings.Code)
	}

	reqRESTTTL := httptest.NewRequest("GET", "/api/v1/network/ttl", nil)
	reqRESTTTL.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wRESTTTL := httptest.NewRecorder()
	handler.ServeHTTP(wRESTTTL, reqRESTTTL)
	if wRESTTTL.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST /network/ttl, got %d", wRESTTTL.Code)
	}

	reqLangCancel := httptest.NewRequest("POST", "/cgi-bin/quecmanager/system/language-packs/install_cancel.sh", nil)
	wLangCancel := httptest.NewRecorder()
	handler.ServeHTTP(wLangCancel, reqLangCancel)
	if wLangCancel.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI system/language-packs/install_cancel.sh, got %d", wLangCancel.Code)
	}

	// Test POST /network/ethernet.sh (CGI)
	reqEthPostCGI := httptest.NewRequest("POST", "/cgi-bin/quecmanager/network/ethernet.sh", strings.NewReader(`{"speed_limit":"1000"}`))
	wEthPostCGI := httptest.NewRecorder()
	handler.ServeHTTP(wEthPostCGI, reqEthPostCGI)
	if wEthPostCGI.Code != http.StatusOK {
		t.Errorf("expected status 200 for CGI network/ethernet.sh POST, got %d", wEthPostCGI.Code)
	}

	// Test POST /network/ethernet (REST)
	reqEthPostREST := httptest.NewRequest("POST", "/api/v1/network/ethernet", strings.NewReader(`{"speed_limit":2500}`))
	reqEthPostREST.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wEthPostREST := httptest.NewRecorder()
	handler.ServeHTTP(wEthPostREST, reqEthPostREST)
	if wEthPostREST.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST /network/ethernet POST, got %d", wEthPostREST.Code)
	}

	// Test /cellular/profiles/deactivate (REST)
	reqDeactREST := httptest.NewRequest("POST", "/api/v1/cellular/profiles/deactivate", nil)
	reqDeactREST.Header.Set("Authorization", "Bearer "+loginResp.Token)
	wDeactREST := httptest.NewRecorder()
	handler.ServeHTTP(wDeactREST, reqDeactREST)
	if wDeactREST.Code != http.StatusOK {
		t.Errorf("expected status 200 for REST /cellular/profiles/deactivate, got %d", wDeactREST.Code)
	}

	// Test /locales-packs/ static file server
	testPackDir := filepath.Join(tempDir, "locales-packs", "zh")
	_ = os.MkdirAll(testPackDir, 0755)
	_ = os.WriteFile(filepath.Join(testPackDir, "common.json"), []byte(`{"hello":"world"}`), 0644)

	reqLocales := httptest.NewRequest("GET", "/locales-packs/zh/common.json", nil)
	wLocales := httptest.NewRecorder()
	handler.ServeHTTP(wLocales, reqLocales)
	if wLocales.Code != http.StatusOK {
		t.Errorf("expected status 200 for /locales-packs/zh/common.json, got %d", wLocales.Code)
	}
}
