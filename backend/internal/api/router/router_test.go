package router

import (
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Engine:    eng,
		Poller:    poller,
		Prober:    prober,
		Watchdog:  watchdog,
		ConfigMgr: cfgMgr,
		Identity:  id,
		DistFS:    testFS,
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
}
