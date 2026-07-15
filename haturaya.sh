#!/bin/bash
set -e

script_dir="$(cd "$(dirname "$0")" && pwd)"
binary="$script_dir/haturaya-c2"

usage() {
    echo "Usage: $0 <ip> <c2_port> <web_port>"
    echo "Example: $0 0.0.0.0 9999 9090"
    exit 1
}

if [[ $# -ne 3 ]]; then
    echo "Error: Missing required arguments."
    usage
fi

# Build if binary missing or any .go file is newer
needs_build=false
if [ ! -f "$binary" ]; then
    needs_build=true
elif find "$script_dir" -name "*.go" -newer "$binary" -print -quit 2>/dev/null | grep -q .; then
    needs_build=true
fi

if $needs_build; then
    echo "[*] Building Haturaya C2..."
    cd "$script_dir"
    go build -o "$binary" . && echo "[+] Build complete"
fi

exec "$binary" "$1" "$2" "$3"
