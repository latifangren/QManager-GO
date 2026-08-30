package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// AuthHandler manages session tokens and authentication.
type AuthHandler struct {
	mu       sync.RWMutex
	password string
	tokens   map[string]time.Time
	timeout  time.Duration
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(defaultPassword string) *AuthHandler {
	if defaultPassword == "" {
		defaultPassword = "admin"
	}
	return &AuthHandler{
		password: defaultPassword,
		tokens:   make(map[string]time.Time),
		timeout:  24 * time.Hour,
	}
}

type LoginRequest struct {
	Password string `json:"password"`
}

// Login verifies password and generates a bearer token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	h.mu.RLock()
	correct := req.Password == h.password
	h.mu.RUnlock()

	if !correct {
		Error(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	h.mu.Lock()
	h.tokens[token] = time.Now().Add(h.timeout)
	h.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "qm_auth_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(h.timeout),
		HttpOnly: true,
	})

	Success(w, map[string]interface{}{
		"token":     token,
		"expires":   time.Now().Add(h.timeout).Unix(),
		"role":      "admin",
		"auth_type": "session",
	})
}

// Check validates token in header or cookie.
func (h *AuthHandler) Check(w http.ResponseWriter, r *http.Request) {
	token := h.extractToken(r)
	if token == "" || !h.validateToken(token) {
		Error(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	Success(w, map[string]interface{}{
		"authenticated": true,
		"role":          "admin",
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
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

	Success(w, map[string]string{"message": "Logged out successfully"})
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
		if len(auth) > 7 && auth[:7] == "Bearer " {
			return auth[7:]
		}
	}
	if cookie, err := r.Cookie("qm_auth_token"); err == nil {
		return cookie.Value
	}
	return ""
}
