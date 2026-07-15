#!/bin/bash
set -e

echo "[*] Starting victim services..."
service cron start
service apache2 start
/usr/sbin/sshd

echo "[+] Services ready — victim is alive"
echo ""
cat /etc/motd

# Keep container running
exec tail -f /dev/null
