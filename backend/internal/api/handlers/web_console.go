package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"tty"},
}

const (
	msgOutput = '0' // 0x30
	msgInput  = '0' // 0x30
	msgResize = '1' // 0x31
	msgJSON   = '{' // 0x7B
)

// WebConsoleHandler provides a native Go PTY WebSocket bridge compatible with ttyd/xterm.js.
type WebConsoleHandler struct{}

// NewWebConsoleHandler creates a new WebConsoleHandler.
func NewWebConsoleHandler() *WebConsoleHandler {
	return &WebConsoleHandler{}
}

// HandleWS handles the WebSocket connection for Web Console at /console/ws.
func (h *WebConsoleHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Default shell: /bin/sh (or /bin/bash if available)
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"HOME=/home/root",
		"USER=root",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usrdata/root/bin",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		cmd = exec.Command(shell)
		cmd.Env = append(os.Environ(),
			"TERM=xterm-256color",
			"HOME=/home/root",
			"USER=root",
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usrdata/root/bin",
		)
		ptmx, err = pty.Start(cmd)
		if err != nil {
			return
		}
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	var writeMu sync.Mutex

	// Goroutine: PTY Output -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				outMsg := make([]byte, 1+n)
				outMsg[0] = msgOutput
				copy(outMsg[1:], buf[:n])

				writeMu.Lock()
				err := conn.WriteMessage(websocket.BinaryMessage, outMsg)
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	// Loop: WebSocket -> PTY Input / Resize
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if len(data) == 0 {
			continue
		}

		cmdByte := data[0]

		switch {
		case cmdByte == msgJSON:
			// Handshake: {"AuthToken":"","columns":80,"rows":24}
			var hs struct {
				Columns uint16 `json:"columns"`
				Rows    uint16 `json:"rows"`
			}
			if err := json.Unmarshal(data, &hs); err == nil && hs.Columns > 0 && hs.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Cols: hs.Columns,
					Rows: hs.Rows,
				})
			}

		case cmdByte == msgInput && (messageType == websocket.BinaryMessage || messageType == websocket.TextMessage):
			// Input data from terminal
			if len(data) > 1 {
				_, _ = ptmx.Write(data[1:])
			}

		case cmdByte == msgResize && (messageType == websocket.BinaryMessage || messageType == websocket.TextMessage):
			// Resize event: {"columns": cols, "rows": rows}
			var rs struct {
				Columns uint16 `json:"columns"`
				Rows    uint16 `json:"rows"`
			}
			if err := json.Unmarshal(data[1:], &rs); err == nil && rs.Columns > 0 && rs.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Cols: rs.Columns,
					Rows: rs.Rows,
				})
			}
		}
	}
}
