package c2

import (
	"fmt"
	"log"
	"net"

	tea "github.com/charmbracelet/bubbletea"

	"haturaya/internal/agent"
	hcrypto "haturaya/internal/crypto"
)

// Server manages the TCP listener and bridges incoming connections to the TUI.
type Server struct {
	host     string
	port     int
	webPort  int
	cipher   *hcrypto.Cipher
	registry *agent.Registry
	prog     *tea.Program // set after the TUI program is created
}

// New creates a new Server with its own agent registry.
func New(host string, port, webPort int, ciph *hcrypto.Cipher) *Server {
	return &Server{
		host:     host,
		port:     port,
		webPort:  webPort,
		cipher:   ciph,
		registry: agent.NewRegistry(),
	}
}

// Registry returns the agent registry (used when constructing the TUI model).
func (s *Server) Registry() *agent.Registry {
	return s.registry
}

// SetProgram wires up the Bubble Tea program so handlers can send messages to it.
// Must be called before ListenAndServe.
func (s *Server) SetProgram(p *tea.Program) {
	s.prog = p
}

// ListenAndServe starts the TCP accept loop. Blocks; call in a goroutine.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.host, s.port))
	if err != nil {
		return fmt.Errorf("listen %s:%d: %w", s.host, s.port, err)
	}
	log.Printf("[c2] TCP listener on %s:%d", s.host, s.port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[c2] accept error: %v", err)
			continue
		}
		go HandleConn(conn, s.cipher, s.registry, s.prog)
	}
}
