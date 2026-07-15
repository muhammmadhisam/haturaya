// Package agent manages the thread-safe registry of connected agents.
package agent

import (
	"net"
	"sort"
	"sync"
)

// Mode describes the transport layer used by an agent.
type Mode int

const (
	// ModeRaw is a plain reverse shell (bash/sh).
	ModeRaw Mode = iota
	// ModeEncrypted is an AES-256-GCM authenticated Python agent.
	ModeEncrypted
)

// String returns a human-readable label for the mode.
func (m Mode) String() string {
	if m == ModeEncrypted {
		return "enc"
	}
	return "raw"
}

// Agent represents a single connected remote host.
type Agent struct {
	ID       string   // unique hex ID assigned at registration
	Conn     net.Conn // live TCP connection
	IP       string   // remote IP address
	OS       string   // "Linux", "Windows", or "Unknown"
	Hostname string   // result of `hostname` command
	Mode     Mode     // raw vs encrypted
	Index    int      // 1-based display index for `use <n>`

	Done      chan struct{} // closed when the agent session ends
	CmdChan   chan string   // operator commands sent to the handler goroutine
	closeOnce sync.Once    // ensures Done is closed exactly once
}

// Close signals the agent handler goroutine to exit and closes the connection.
// Safe to call multiple times.
func (a *Agent) Close() {
	a.closeOnce.Do(func() {
		close(a.Done)
		a.Conn.Close()
	})
}

// Registry is a concurrency-safe store of active agents.
type Registry struct {
	mu      sync.Mutex
	agents  map[string]*Agent
	counter int // monotonically increasing index source
}

// NewRegistry returns an initialised, empty Registry.
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]*Agent)}
}

// Add inserts an agent into the registry, assigning its Index automatically.
func (r *Registry) Add(a *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter++
	a.Index = r.counter
	r.agents[a.ID] = a
}

// Remove deletes the agent with the given ID from the registry.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
}

// Get returns the agent with the given ID, if it exists.
func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	return a, ok
}

// GetByIndex returns the agent whose 1-based Index matches n.
func (r *Registry) GetByIndex(n int) (*Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.agents {
		if a.Index == n {
			return a, true
		}
	}
	return nil, false
}

// List returns all agents sorted by their Index (ascending).
func (r *Registry) List() []*Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out
}

// Count returns the number of currently registered agents.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.agents)
}

// NextIndex returns what the next agent's Index will be without advancing the counter.
func (r *Registry) NextIndex() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counter + 1
}
