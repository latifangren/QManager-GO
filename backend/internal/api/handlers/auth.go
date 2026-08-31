package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var defaultAuthFilePath = "/etc/qmanager/auth.json"

// AuthStorage represents the persistent credential record in /etc/qmanager/auth.json.
type AuthStorage struct {
	Hash    string `json:"hash"`
	Salt    string `json:"salt"`
	Version int    `json:"version"`
}

// AuthHandler manages session tokens, password verification, and credentials persistence.
type AuthHandler struct {
	mu            sync.RWMutex
	authPath      string
	hash          string
	salt          string
	setupRequired bool
	tokens        map[string]time.Time
	timeout       time.Duration
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(defaultPassword string, optionalPath ...string) *AuthHandler {
	path := defaultAuthFilePath
	if len(optionalPath) > 0 && optionalPath[0] != "" {
		path = optionalPath[0]
	}

	h := &AuthHandler{
		authPath:      path,
		tokens:        make(map[string]time.Time),
		timeout:       24 * time.Hour,
		setupRequired: true,
	}

	if defaultPassword != "" && defaultPassword != "admin" {
		h.setPasswordInternal(defaultPassword)
		h.setupRequired = false
	} else if err := h.loadAuthFile(); err == nil {
		h.setupRequired = false
	}

	return h
}

// SetAuthFilePath updates the auth storage file path and reloads credentials.
func (h *AuthHandler) SetAuthFilePath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authPath = path
	if err := h.loadAuthFile(); err == nil {
		h.setupRequired = false
	} else {
		h.setupRequired = true
		h.hash = ""
		h.salt = ""
	}
}

// SetPassword sets a password directly in-memory and marks setup as completed.
func (h *AuthHandler) SetPassword(password string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.setPasswordInternal(password)
	h.setupRequired = false
}

// SetSetupRequired sets the setupRequired flag.
func (h *AuthHandler) SetSetupRequired(req bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.setupRequired = req
}

// IsSetupRequired returns the setupRequired flag.
func (h *AuthHandler) IsSetupRequired() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.setupRequired
}

func (h *AuthHandler) setPasswordInternal(password string) {
	h.salt = generateSalt()
	h.hash = hashPassword(password, h.salt)
}

func hashPassword(password, salt string) string {
	hasher := sha256.New()
	hasher.Write([]byte(salt + password))
	return hex.EncodeToString(hasher.Sum(nil))
}

func generateSalt() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *AuthHandler) verifyPassword(password string) bool {
	if h.hash == "" || h.salt == "" {
		return false
	}
	computed := hashPassword(password, h.salt)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(h.hash)) == 1
}

func (h *AuthHandler) loadAuthFile() error {
	data, err := os.ReadFile(h.authPath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("auth file is empty")
	}

	var storage AuthStorage
	if err := json.Unmarshal(data, &storage); err != nil {
		return err
	}
	if storage.Hash == "" || storage.Salt == "" {
		return errors.New("invalid auth storage payload")
	}

	h.hash = storage.Hash
	h.salt = storage.Salt
	return nil
}

func (h *AuthHandler) saveAuthFile(hash, salt string) error {
	dir := filepath.Dir(h.authPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	storage := AuthStorage{
		Hash:    hash,
		Salt:    salt,
		Version: 1,
	}

	data, err := json.MarshalIndent(storage, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth storage: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", h.authPath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open temp auth file %s: %w", tmpFile, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to write auth data: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to fsync auth file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to close temp auth file: %w", err)
	}

	if err := os.Rename(tmpFile, h.authPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp auth file to %s: %w", h.authPath, err)
	}

	_ = os.Chmod(h.authPath, 0600)
	return nil
}

type LoginRequest struct {
	Password        string `json:"password"`
	Confirm         string `json:"confirm,omitempty"`
	ConfirmPassword string `json:"confirm_password,omitempty"`
}

// Login verifies password or completes first-time setup, and generates a bearer token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	h.mu.Lock()
	setupReq := h.setupRequired && h.hash == ""

	if setupReq {
		if len(req.Password) < 6 {
			h.mu.Unlock()
			Error(w, http.StatusBadRequest, "Password must be at least 6 characters")
			return
		}

		confirm := req.Confirm
		if confirm == "" {
			confirm = req.ConfirmPassword
		}
		if confirm != "" && req.Password != confirm {
			h.mu.Unlock()
			Error(w, http.StatusBadRequest, "Passwords do not match")
			return
		}

		salt := generateSalt()
		hash := hashPassword(req.Password, salt)
		_ = h.saveAuthFile(hash, salt)

		h.hash = hash
		h.salt = salt
		h.setupRequired = false
	} else {
		if !h.verifyPassword(req.Password) {
			h.mu.Unlock()
			Error(w, http.StatusUnauthorized, "Invalid password")
			return
		}
	}

	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	expires := time.Now().Add(h.timeout)
	h.tokens[token] = expires
	h.mu.Unlock()

	cookie := &http.Cookie{
		Name:     "qm_auth_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)

	http.SetCookie(w, &http.Cookie{
		Name:     "qm_logged_in",
		Value:    "1",
		Path:     "/",
		MaxAge:   86400,
		Expires:  expires,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})

	dataMap := map[string]interface{}{
		"token":           token,
		"expires":         expires.Unix(),
		"role":            "admin",
		"auth_type":       "session",
		"authenticated":   true,
		"setup_required":  false,
		"setup_completed": setupReq,
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"authenticated":   true,
		"token":           token,
		"expires":         expires.Unix(),
		"role":            "admin",
		"setup_required":  false,
		"setup_completed": setupReq,
		"auth_type":       "session",
		"data":            dataMap,
	})
}

// Check validates token in header or cookie or returns setup_required status.
func (h *AuthHandler) Check(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	setupReq := h.setupRequired
	h.mu.RUnlock()

	if setupReq {
		JSON(w, http.StatusOK, map[string]interface{}{
			"success":        false,
			"authenticated":  false,
			"setup_required": true,
			"data": map[string]interface{}{
				"authenticated":  false,
				"setup_required": true,
			},
		})
		return
	}

	token := h.extractToken(r)
	if token == "" || !h.validateToken(token) {
		JSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success":        false,
			"authenticated":  false,
			"setup_required": false,
			"error":          "Not authenticated",
		})
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"authenticated":  true,
		"role":           "admin",
		"setup_required": false,
		"data": map[string]interface{}{
			"authenticated":  true,
			"role":           "admin",
			"setup_required": false,
		},
	})
}

// Logout invalidates the active session token.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := h.extractToken(r)
	if token != "" {
		h.mu.Lock()
		delete(h.tokens, token)
		h.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "qm_auth_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "qm_logged_in",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
	})
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password,omitempty"`
}

// ChangePassword updates the administrator password after validating current password.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.verifyPassword(req.CurrentPassword) {
		Error(w, http.StatusBadRequest, "Current password is incorrect")
		return
	}

	if len(req.NewPassword) < 6 {
		Error(w, http.StatusBadRequest, "New password must be at least 6 characters")
		return
	}

	if req.ConfirmPassword != "" && req.NewPassword != req.ConfirmPassword {
		Error(w, http.StatusBadRequest, "Passwords do not match")
		return
	}

	salt := generateSalt()
	hash := hashPassword(req.NewPassword, salt)
	if err := h.saveAuthFile(hash, salt); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save new password: %v", err))
		return
	}

	h.hash = hash
	h.salt = salt
	h.setupRequired = false
	h.tokens = make(map[string]time.Time)

	Success(w, map[string]string{
		"message": "Password changed successfully",
	})
}

type ChangeSSHPasswordRequest struct {
	Password string `json:"password"`
}

// ChangeSSHPassword updates the system root SSH password.
func (h *AuthHandler) ChangeSSHPassword(w http.ResponseWriter, r *http.Request) {
	var req ChangeSSHPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Password) < 6 {
		Error(w, http.StatusBadRequest, "Password must be at least 6 characters")
		return
	}

	if runtime.GOOS == "linux" {
		cmd := exec.Command("chpasswd")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("root:%s\n", req.Password))
		if err := cmd.Run(); err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update system SSH password: %v", err))
			return
		}
	}

	Success(w, map[string]string{
		"message": "SSH password updated successfully",
	})
}

// Middleware creates an HTTP middleware for authenticating protected routes.
func (h *AuthHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := h.extractToken(r)
		if token == "" || !h.validateToken(token) {
			JSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success":       false,
				"authenticated": false,
				"error":         "Unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateToken is helper for auth middleware.
func (h *AuthHandler) ValidateToken(token string) bool {
	return h.validateToken(token)
}

func (h *AuthHandler) validateToken(token string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	exp, ok := h.tokens[token]
	if !ok {
		return false
	}
	return time.Now().Before(exp)
}

func (h *AuthHandler) extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			return strings.TrimSpace(auth[7:])
		}
	}
	if cookie, err := r.Cookie("qm_auth_token"); err == nil {
		return cookie.Value
	}
	return ""
}
