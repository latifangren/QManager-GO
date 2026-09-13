package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"

	"qmanager/internal/sshd/auth"
)

// Config holds configuration for the native SSH server.
type Config struct {
	ListenAddr string // e.g. ":22" or ":2222"
	KeyDir     string // e.g. "/etc/qmanager/ssh" or "/usrdata/qmanager/ssh"
	ShadowPath string // e.g. "/etc/shadow"
	Shell      string // e.g. "/bin/sh"
}

// Server is a standalone SSH server daemon.
type Server struct {
	cfg      Config
	listener net.Listener
	sshCfg   *ssh.ServerConfig
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewServer initializes the SSH server and prepares host keys.
func NewServer(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":22"
	}
	if cfg.KeyDir == "" {
		cfg.KeyDir = "/etc/qmanager/ssh"
	}
	if cfg.ShadowPath == "" {
		cfg.ShadowPath = "/etc/shadow"
	}
	if cfg.Shell == "" {
		cfg.Shell = "/bin/sh"
		if _, err := os.Stat(cfg.Shell); err != nil {
			cfg.Shell = "/bin/bash"
		}
	}

	signer, err := ensureHostKey(cfg.KeyDir)
	if err != nil {
		// Fallback to /usrdata/qmanager/ssh if unwritable
		cfg.KeyDir = "/usrdata/qmanager/ssh"
		signer, err = ensureHostKey(cfg.KeyDir)
		if err != nil {
			return nil, fmt.Errorf("sshd: failed to load or create host key: %w", err)
		}
	}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			user := c.User()
			ok, err := auth.VerifyShadowPassword(user, string(pass), cfg.ShadowPath)
			if err != nil || !ok {
				return nil, fmt.Errorf("authentication failed for %s", user)
			}
			return nil, nil
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			if checkAuthorizedKey(c.User(), pubKey, cfg.KeyDir) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key for %s", c.User())
		},
	}
	sshConfig.AddHostKey(signer)

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:    cfg,
		sshCfg: sshConfig,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Start boots the SSH server listener in the background.
func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("sshd: listen on %s failed: %w", s.cfg.ListenAddr, err)
	}
	s.listener = l

	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

// Close gracefully shuts down the SSH server.
func (s *Server) Close() error {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("sshd: accept error: %v\n", err)
				continue
			}
		}

		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(netConn net.Conn) {
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.sshCfg)
	if err != nil {
		return
	}
	defer sshConn.Close()

	// Discard global out-of-band requests
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go s.handleSession(channel, requests)
	}
}

func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	var ptmx *os.File
	var cmd *exec.Cmd
	var ptmxMu sync.Mutex

	for req := range requests {
		switch req.Type {
		case "pty-req":
			termLen := binary.BigEndian.Uint32(req.Payload[:4])
			w := binary.BigEndian.Uint32(req.Payload[termLen+4 : termLen+8])
			h := binary.BigEndian.Uint32(req.Payload[termLen+8 : termLen+12])
			ws := &pty.Winsize{
				Rows: uint16(h),
				Cols: uint16(w),
			}

			cmd = exec.Command(s.cfg.Shell)
			cmd.Env = append(os.Environ(), "TERM=xterm-256color", "HOME=/root", "USER=root")

			var err error
			ptmxMu.Lock()
			ptmx, err = pty.StartWithSize(cmd, ws)
			ptmxMu.Unlock()

			if err != nil {
				req.Reply(false, nil)
				return
			}
			req.Reply(true, nil)

		case "window-change":
			if len(req.Payload) >= 8 {
				w := binary.BigEndian.Uint32(req.Payload[0:4])
				h := binary.BigEndian.Uint32(req.Payload[4:8])
				ptmxMu.Lock()
				if ptmx != nil {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(h),
						Cols: uint16(w),
					})
				}
				ptmxMu.Unlock()
			}
			req.Reply(true, nil)

		case "shell":
			if ptmx == nil {
				// Non-PTY interactive shell fallback
				cmd = exec.Command(s.cfg.Shell)
				cmd.Env = append(os.Environ(), "TERM=vt100", "HOME=/root", "USER=root")
				stdin, _ := cmd.StdinPipe()
				stdout, _ := cmd.StdoutPipe()
				stderr, _ := cmd.StderrPipe()

				if err := cmd.Start(); err != nil {
					req.Reply(false, nil)
					return
				}
				req.Reply(true, nil)

				go io.Copy(stdin, channel)
				go io.Copy(channel, stdout)
				go io.Copy(channel.Stderr(), stderr)

				_ = cmd.Wait()
				channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				return
			}

			req.Reply(true, nil)

			// Bridge PTY <-> SSH Channel
			go func() {
				_, _ = io.Copy(ptmx, channel)
			}()
			_, _ = io.Copy(channel, ptmx)

			_ = cmd.Wait()
			ptmxMu.Lock()
			if ptmx != nil {
				ptmx.Close()
			}
			ptmxMu.Unlock()

			channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			return

		case "exec":
			if len(req.Payload) < 4 {
				req.Reply(false, nil)
				return
			}
			cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
			if uint32(len(req.Payload)) < 4+cmdLen {
				req.Reply(false, nil)
				return
			}
			commandStr := string(req.Payload[4 : 4+cmdLen])

			cmd = exec.Command(s.cfg.Shell, "-c", commandStr)
			cmd.Env = append(os.Environ(), "HOME=/root", "USER=root")
			stdin, _ := cmd.StdinPipe()
			stdout, _ := cmd.StdoutPipe()
			stderr, _ := cmd.StderrPipe()

			if err := cmd.Start(); err != nil {
				req.Reply(false, nil)
				return
			}
			req.Reply(true, nil)

			go io.Copy(stdin, channel)
			go io.Copy(channel, stdout)
			go io.Copy(channel.Stderr(), stderr)

			status := 0
			if err := cmd.Wait(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
						status = ws.ExitStatus()
					}
				}
			}

			exitBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(exitBytes, uint32(status))
			channel.SendRequest("exit-status", false, exitBytes)
			return

		case "env":
			req.Reply(true, nil)

		default:
			req.Reply(false, nil)
		}
	}
}

func ensureHostKey(keyDir string) (ssh.Signer, error) {
	_ = os.MkdirAll(keyDir, 0700)
	keyPath := filepath.Join(keyDir, "id_ed25519")

	if data, err := os.ReadFile(keyPath); err == nil && len(data) > 0 {
		return ssh.ParsePrivateKey(data)
	}

	// Generate ED25519 Host Key
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, err
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}

	file, err := os.OpenFile(keyPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if err := pem.Encode(file, pemBlock); err != nil {
		return nil, err
	}

	return ssh.NewSignerFromKey(privKey)
}

func checkAuthorizedKey(user string, pubKey ssh.PublicKey, keyDir string) bool {
	paths := []string{
		filepath.Join("/root/.ssh/authorized_keys"),
		filepath.Join(keyDir, "authorized_keys"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for len(data) > 0 {
			authKey, _, _, rest, err := ssh.ParseAuthorizedKey(data)
			if err != nil {
				break
			}
			if string(authKey.Marshal()) == string(pubKey.Marshal()) {
				return true
			}
			data = rest
		}
	}
	return false
}
