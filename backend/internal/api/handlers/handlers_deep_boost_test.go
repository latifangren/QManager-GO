package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/platform"
	"qmanager/internal/telemetry"
)

func TestHandlers_DeepBoost(t *testing.T) {
	tmpDir := t.TempDir()
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	defer eng.Close()

	cfgPath := filepath.Join(tmpDir, "qmanager.conf")
	cfgMgr, _ := config.NewManager(cfgPath)
	identity := platform.Identity{Model: "RG501Q-EU"}
	poller := telemetry.NewPoller(eng, identity, 100*time.Millisecond)
	_ = poller

	// 1. AuthHandler - Cookie extraction branch
	authH := NewAuthHandler("mypassword")
	loginBody, _ := json.Marshal(LoginRequest{Password: "mypassword"})
	wLogin := httptest.NewRecorder()
	authH.Login(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(loginBody)))

	cookie := wLogin.Result().Cookies()[0]
	reqCookieCheck := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	reqCookieCheck.AddCookie(cookie)
	wCookieCheck := httptest.NewRecorder()
	authH.Check(wCookieCheck, reqCookieCheck)
	if wCookieCheck.Code != http.StatusOK {
		t.Errorf("expected 200 via cookie auth, got %d", wCookieCheck.Code)
	}

	// 2. SIMProfileHandler - Full lifecycle
	simProfH := NewSIMProfileHandler(eng)
	simProfH.SetStoragePaths(
		filepath.Join(tmpDir, "profiles"),
		filepath.Join(tmpDir, "active_profile"),
		filepath.Join(tmpDir, "profile_state.json"),
	)

	// List
	reqList := httptest.NewRequest(http.MethodGet, "/api/cellular/profiles", nil)
	wList := httptest.NewRecorder()
	simProfH.List(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("List profiles returned %d, want 200", wList.Code)
	}

	// Save new profile
	bodySaveProf, _ := json.Marshal(SIMProfile{
		Name:  "Test Profile",
		ICCID: "8986000000000000001",
		Settings: ProfileSettings{
			APN:     "internet",
			PDPType: "IPV4V6",
			CID:     1,
		},
	})
	reqSaveProf := httptest.NewRequest(http.MethodPost, "/api/cellular/profiles", bytes.NewBuffer(bodySaveProf))
	wSaveProf := httptest.NewRecorder()
	simProfH.Save(wSaveProf, reqSaveProf)
	if wSaveProf.Code != http.StatusOK {
		t.Fatalf("Save profile returned %d, want 200", wSaveProf.Code)
	}

	var saveProfResp map[string]interface{}
	_ = json.NewDecoder(wSaveProf.Body).Decode(&saveProfResp)
	profID := saveProfResp["id"].(string)

	// Get profile
	reqGetProf := httptest.NewRequest(http.MethodGet, "/api/cellular/profiles?id="+profID, nil)
	wGetProf := httptest.NewRecorder()
	simProfH.Get(wGetProf, reqGetProf)
	if wGetProf.Code != http.StatusOK {
		t.Fatalf("Get profile returned %d, want 200", wGetProf.Code)
	}

	// Apply profile
	mock.SetResponse(`AT+CGDCONT=1,"IPV4V6","internet"`, "OK")
	mock.SetResponse("AT+CGATT=0", "OK")
	mock.SetResponse("AT+CGATT=1", "OK")
	bodyApply, _ := json.Marshal(map[string]string{"id": profID})
	reqApply := httptest.NewRequest(http.MethodPost, "/api/cellular/profiles/apply", bytes.NewBuffer(bodyApply))
	wApply := httptest.NewRecorder()
	simProfH.Apply(wApply, reqApply)
	if wApply.Code != http.StatusOK {
		t.Fatalf("Apply profile returned %d, want 200", wApply.Code)
	}

	// ApplyStatus query
	reqApplyStatus := httptest.NewRequest(http.MethodGet, "/api/cellular/profiles/apply-status", nil)
	wApplyStatus := httptest.NewRecorder()
	simProfH.ApplyStatus(wApplyStatus, reqApplyStatus)
	if wApplyStatus.Code != http.StatusOK {
		t.Fatalf("ApplyStatus returned %d, want 200", wApplyStatus.Code)
	}

	// Delete profile
	bodyDelProf, _ := json.Marshal(map[string]string{"id": profID})
	reqDelProf := httptest.NewRequest(http.MethodPost, "/api/cellular/profiles/delete.sh", bytes.NewBuffer(bodyDelProf))
	wDelProf := httptest.NewRecorder()
	simProfH.Delete(wDelProf, reqDelProf)
	if wDelProf.Code != http.StatusOK {
		t.Fatalf("Delete profile returned %d, want 200", wDelProf.Code)
	}

	// 3. LogsHandler - ModemSubsys diagnostic
	logsH := NewLogsHandler()
	reqSubsys := httptest.NewRequest(http.MethodGet, "/api/system/subsys", nil)
	wSubsys := httptest.NewRecorder()
	logsH.ModemSubsys(wSubsys, reqSubsys)
	if wSubsys.Code != http.StatusOK {
		t.Fatalf("ModemSubsys returned %d, want 200", wSubsys.Code)
	}

	// 4. VideoOptimizerHandler - hostlist and install actions
	dpiConfigFile = filepath.Join(tmpDir, "dpi_config.json")
	dpiHostlistFile = filepath.Join(tmpDir, "dpi_hostlist.txt")
	dpiVerifyFile = filepath.Join(tmpDir, "dpi_verify.json")
	dpiInstallFile = filepath.Join(tmpDir, "dpi_install.json")
	dpiH := NewVideoOptimizerHandler()

	// GET section=hostlist
	reqHostlist := httptest.NewRequest(http.MethodGet, "/api/network/video-optimizer?section=hostlist", nil)
	wHostlist := httptest.NewRecorder()
	dpiH.HandleGet(wHostlist, reqHostlist)
	if wHostlist.Code != http.StatusOK {
		t.Fatalf("HandleGet section=hostlist returned %d, want 200", wHostlist.Code)
	}

	// POST save_hostlist
	bodyHostlist, _ := json.Marshal(VideoOptimizerSavePayload{
		Action:  "save_hostlist",
		Domains: []string{"example.com", "speedtest.net"},
	})
	reqSaveHostlist := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyHostlist))
	wSaveHostlist := httptest.NewRecorder()
	dpiH.HandlePost(wSaveHostlist, reqSaveHostlist)
	if wSaveHostlist.Code != http.StatusOK {
		t.Fatalf("HandlePost save_hostlist returned %d, want 200", wSaveHostlist.Code)
	}

	// POST verify & uninstall
	bodyVerify, _ := json.Marshal(VideoOptimizerSavePayload{Action: "verify"})
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/network/video-optimizer", bytes.NewBuffer(bodyVerify))
	wVerify := httptest.NewRecorder()
	dpiH.HandlePost(wVerify, reqVerify)
	if wVerify.Code != http.StatusOK {
		t.Fatalf("HandlePost verify returned %d, want 200", wVerify.Code)
	}

	// 5. CellularApnHandler - handleDeactivate & handleToggle
	apnSettingPath = filepath.Join(tmpDir, "apn_setting.json")
	apnNamesPath = filepath.Join(tmpDir, "apn_names.json")
	apnH := NewCellularApnHandler(eng, cfgMgr)
	mock.SetResponse(`AT+CGDCONT=1,"IPV4V6",""`, "OK")
	bodyDeact, _ := json.Marshal(ApnSavePayload{Action: "deactivate"})
	reqDeact := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyDeact))
	wDeact := httptest.NewRecorder()
	apnH.SaveAPN(wDeact, reqDeact)
	if wDeact.Code != http.StatusOK {
		t.Fatalf("SaveAPN deactivate returned %d, want 200", wDeact.Code)
	}

	mock.SetResponse("AT+CGACT=1,1", "OK")
	en := true
	bodyToggle, _ := json.Marshal(ApnSavePayload{
		Action:  "toggle",
		Cid:     1,
		Enabled: &en,
	})
	reqToggle := httptest.NewRequest(http.MethodPost, "/api/cellular/apn", bytes.NewBuffer(bodyToggle))
	wToggle := httptest.NewRecorder()
	apnH.SaveAPN(wToggle, reqToggle)
	if wToggle.Code != http.StatusOK {
		t.Fatalf("SaveAPN toggle returned %d, want 200", wToggle.Code)
	}

	// Helper pdpToCtxtype & authToAT
	if pdpToCtxtype("IPV4") != "1" || pdpToCtxtype("IPV6") != "2" || pdpToCtxtype("IPV4V6") != "3" {
		t.Errorf("pdpToCtxtype mismatch")
	}
	if authToAT("PAP") != "1" || authToAT("CHAP") != "2" || authToAT("NONE") != "0" {
		t.Errorf("authToAT mismatch")
	}

	// 6. Frequency calculator helper
	f1 := nrArfcnToFrequency(-1)
	f2 := nrArfcnToFrequency(3000000)
	if f1 != 0 || f2 == 0 {
		t.Errorf("nrArfcnToFrequency boundary check failed: f1=%f, f2=%f", f1, f2)
	}
}
