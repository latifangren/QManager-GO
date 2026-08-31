package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAuthHandler_LifecycleAndPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	// 1. Initial State: Setup Required when file doesn't exist
	h := NewAuthHandler("", authPath)
	if !h.IsSetupRequired() {
		t.Fatalf("expected setup_required to be true initially")
	}

	// Check endpoint returns setup_required: true
	reqCheck := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	wCheck := httptest.NewRecorder()
	h.Check(wCheck, reqCheck)
	if wCheck.Code != http.StatusOK {
		t.Fatalf("expected 200 for Check when setup required, got %d", wCheck.Code)
	}
	var checkResp struct {
		Success       bool `json:"success"`
		Authenticated bool `json:"authenticated"`
		SetupRequired bool `json:"setup_required"`
	}
	_ = json.NewDecoder(wCheck.Body).Decode(&checkResp)
	if !checkResp.SetupRequired || checkResp.Authenticated {
		t.Fatalf("expected setup_required=true, authenticated=false, got %+v", checkResp)
	}

	// 2. Setup via Login: Short password rejection
	bodyShort, _ := json.Marshal(LoginRequest{Password: "123", Confirm: "123"})
	wShort := httptest.NewRecorder()
	h.Login(wShort, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBuffer(bodyShort)))
	if wShort.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", wShort.Code)
	}

	// Mismatched confirm password rejection
	bodyMismatch, _ := json.Marshal(LoginRequest{Password: "password123", Confirm: "different123"})
	wMismatch := httptest.NewRecorder()
	h.Login(wMismatch, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBuffer(bodyMismatch)))
	if wMismatch.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for mismatched passwords, got %d", wMismatch.Code)
	}

	// Valid setup
	bodySetup, _ := json.Marshal(LoginRequest{Password: "secret123", Confirm: "secret123"})
	wSetup := httptest.NewRecorder()
	h.Login(wSetup, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBuffer(bodySetup)))
	if wSetup.Code != http.StatusOK {
		t.Fatalf("expected 200 for successful setup, got %d: %s", wSetup.Code, wSetup.Body.String())
	}
	if h.IsSetupRequired() {
		t.Fatalf("expected setup_required to be false after setup")
	}

	// 3. Reload from file in a new handler instance
	h2 := NewAuthHandler("", authPath)
	if h2.IsSetupRequired() {
		t.Fatalf("expected h2 to load persisted auth and have setup_required=false")
	}

	// Check without token on h2 returns 401
	wCheck2 := httptest.NewRecorder()
	h2.Check(wCheck2, httptest.NewRequest(http.MethodGet, "/api/auth/check", nil))
	if wCheck2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated check, got %d", wCheck2.Code)
	}

	// Login with correct password on h2
	bodyLogin, _ := json.Marshal(LoginRequest{Password: "secret123"})
	wLogin := httptest.NewRecorder()
	h2.Login(wLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(bodyLogin)))
	if wLogin.Code != http.StatusOK {
		t.Fatalf("expected 200 for correct password login, got %d", wLogin.Code)
	}

	var loginData struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(wLogin.Body).Decode(&loginData)
	if loginData.Token == "" {
		t.Fatalf("expected token in login response")
	}

	// ValidateToken
	if !h2.ValidateToken(loginData.Token) {
		t.Fatalf("expected ValidateToken=true for valid token")
	}

	// Check with Bearer token
	reqAuthCheck := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	reqAuthCheck.Header.Set("Authorization", "Bearer "+loginData.Token)
	wAuthCheck := httptest.NewRecorder()
	h2.Check(wAuthCheck, reqAuthCheck)
	if wAuthCheck.Code != http.StatusOK {
		t.Errorf("expected 200 with Bearer token, got %d", wAuthCheck.Code)
	}

	// 4. ChangePassword
	// Bad current password
	bodyChangeBad, _ := json.Marshal(ChangePasswordRequest{
		CurrentPassword: "wrong",
		NewPassword:     "newsecret123",
		ConfirmPassword: "newsecret123",
	})
	wChangeBad := httptest.NewRecorder()
	h2.ChangePassword(wChangeBad, httptest.NewRequest(http.MethodPost, "/api/auth/password", bytes.NewBuffer(bodyChangeBad)))
	if wChangeBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for incorrect current password, got %d", wChangeBad.Code)
	}

	// Successful password change
	bodyChangeGood, _ := json.Marshal(ChangePasswordRequest{
		CurrentPassword: "secret123",
		NewPassword:     "newsecret123",
		ConfirmPassword: "newsecret123",
	})
	wChangeGood := httptest.NewRecorder()
	h2.ChangePassword(wChangeGood, httptest.NewRequest(http.MethodPost, "/api/auth/password", bytes.NewBuffer(bodyChangeGood)))
	if wChangeGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for successful password change, got %d: %s", wChangeGood.Code, wChangeGood.Body.String())
	}

	// Old token invalidated
	if h2.ValidateToken(loginData.Token) {
		t.Errorf("expected old token to be invalidated after password change")
	}

	// Login with old password fails
	wLoginOld := httptest.NewRecorder()
	h2.Login(wLoginOld, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(bodyLogin)))
	if wLoginOld.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for old password, got %d", wLoginOld.Code)
	}

	// Login with new password succeeds
	bodyNewLogin, _ := json.Marshal(LoginRequest{Password: "newsecret123"})
	wLoginNew := httptest.NewRecorder()
	h2.Login(wLoginNew, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(bodyNewLogin)))
	if wLoginNew.Code != http.StatusOK {
		t.Fatalf("expected 200 for new password, got %d", wLoginNew.Code)
	}

	// 5. ChangeSSHPassword
	bodySSH, _ := json.Marshal(ChangeSSHPasswordRequest{Password: "rootnewpass123"})
	wSSH := httptest.NewRecorder()
	h2.ChangeSSHPassword(wSSH, httptest.NewRequest(http.MethodPost, "/api/auth/ssh_password", bytes.NewBuffer(bodySSH)))
	if wSSH.Code != http.StatusOK {
		t.Errorf("expected 200 for SSH password change, got %d", wSSH.Code)
	}

	// 6. Logout
	wLogout := httptest.NewRecorder()
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.Header.Set("Authorization", "Bearer "+loginData.Token)
	h2.Logout(wLogout, reqLogout)
	if wLogout.Code != http.StatusOK {
		t.Errorf("expected 200 for logout, got %d", wLogout.Code)
	}
}
