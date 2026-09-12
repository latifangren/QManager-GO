package sshd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"qmanager/internal/config"
)

// Manager coordinates the lifecycle of the SSH server daemon according to system config.
type Manager struct {
	cfgMgr *config.Manager
	keyDir string
	mu     sync.Mutex
	server *Server
}

// Status represents the current operational state of the SSH service.
type Status struct {
	Enabled        bool   `json:"enabled"`
	Port           int    `json:"port"`
	Running        bool   `json:"running"`
	AuthorizedKeys string `json:"authorized_keys"`
}

// NewManager creates a manager for the SSH daemon.
func NewManager(cfgMgr *config.Manager, keyDir string) *Manager {
	if keyDir == "" {
		keyDir = "/etc/qmanager/ssh"
	}
	return &Manager{
		cfgMgr: cfgMgr,
		keyDir: keyDir,
	}
}

// Start boots the SSH server if enabled in config.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.cfgMgr.Get()
	// If unconfigured in older conf files (Port == 0 and Enabled == 0), default to enabled = true
	enabled := c.SSH.Enabled == 1 || (c.SSH.Port == 0 && c.SSH.Enabled == 0)
	if !enabled {
		log.Println("🔑 Native SSH Server is disabled in config")
		return nil
	}

	port := c.SSH.Port
	if port <= 0 || port > 65535 {
		port = 22
	}

	// Sync authorized keys to disk if present in config
	if strings.TrimSpace(c.SSH.AuthorizedKeys) != "" {
		_ = m.saveAuthorizedKeysDisk(c.SSH.AuthorizedKeys)
	}

	srv, err := NewServer(Config{
		ListenAddr: fmt.Sprintf(":%d", port),
		KeyDir:     m.keyDir,
		ShadowPath: "/etc/shadow",
		Shell:      "/bin/sh",
	})
	if err != nil {
		return fmt.Errorf("failed to init ssh server: %w", err)
	}

	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start ssh listener on port %d: %w", port, err)
	}

	m.server = srv
	log.Printf("🔑 Native SSH Server active on :%d\n", port)
	return nil
}

// Stop shuts down the active SSH server listener.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.server != nil {
		err := m.server.Close()
		m.server = nil
		log.Println("🛑 Native SSH Server stopped")
		return err
	}
	return nil
}

// ApplySettings updates config and restarts/stops the SSH daemon as requested.
func (m *Manager) ApplySettings(enabled bool, port int, authorizedKeys string) error {
	if port <= 0 || port > 65535 {
		port = 22
	}

	// Update persistent config
	err := m.cfgMgr.Update(func(c *config.Config) {
		if enabled {
			c.SSH.Enabled = 1
		} else {
			c.SSH.Enabled = 0
		}
		c.SSH.Port = port
		c.SSH.AuthorizedKeys = strings.TrimSpace(authorizedKeys)
	})
	if err != nil {
		return fmt.Errorf("failed to save ssh config: %w", err)
	}

	// Persist authorized keys to file
	_ = m.saveAuthorizedKeysDisk(authorizedKeys)

	// Reload daemon
	m.mu.Lock()
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	m.mu.Unlock()

	if enabled {
		return m.Start()
	}
	return nil
}

// GetStatus returns the current status and config for the SSH service.
func (m *Manager) GetStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.cfgMgr.Get()
	enabled := c.SSH.Enabled == 1 || (c.SSH.Port == 0 && c.SSH.Enabled == 0)
	port := c.SSH.Port
	if port <= 0 || port > 65535 {
		port = 22
	}

	keys := c.SSH.AuthorizedKeys
	if keys == "" {
		keys = m.readAuthorizedKeysDisk()
	}

	return Status{
		Enabled:        enabled,
		Port:           port,
		Running:        m.server != nil,
		AuthorizedKeys: keys,
	}
}

func (m *Manager) saveAuthorizedKeysDisk(keys string) error {
	_ = os.MkdirAll(m.keyDir, 0700)
	keyFile := filepath.Join(m.keyDir, "authorized_keys")
	return os.WriteFile(keyFile, []byte(strings.TrimSpace(keys)+"\n"), 0600)
}

func (m *Manager) readAuthorizedKeysDisk() string {
	paths := []string{
		filepath.Join(m.keyDir, "authorized_keys"),
		"/root/.ssh/authorized_keys",
	}
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			return string(data)
		}
	}
	return ""
}
