#!/usr/bin/env bash
# First-time setup for OSCP Lab Tracker.
# Run it from inside the repo:   chmod +x setup.sh && ./setup.sh
set -e

OSCP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
cd "$OSCP_DIR"

say()  { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33m!   %s\033[0m\n' "$1"; }

echo "OSCP Lab Tracker — setup"
echo "repo: $OSCP_DIR"
echo

# 1. Go toolchain -----------------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
  warn "Go is not installed (needed to build the binary)."
  if command -v apt >/dev/null 2>&1; then
    read -rp "Install Go now with 'sudo apt install golang-go'? [y/N] " ans
    case "$ans" in
      y|Y) sudo apt update && sudo apt install -y golang-go ;;
      *)   echo "Install Go from https://go.dev/dl/ and re-run ./setup.sh"; exit 1 ;;
    esac
  else
    echo "Install Go from https://go.dev/dl/ and re-run ./setup.sh"; exit 1
  fi
fi
say "Go found: $(go version)"

# 2. Build the binary -------------------------------------------------------
say "Building the oscp binary..."
go build -o oscp oscp.go
say "Built: $OSCP_DIR/oscp"

# 3. Config -----------------------------------------------------------------
if [ -f oscp.conf ]; then
  say "oscp.conf already exists — leaving it untouched."
else
  cp oscp.conf.example oscp.conf
  say "Created oscp.conf — edit it later to set your site URL / default domain."
fi

# 4. Shell integration ------------------------------------------------------
case "${SHELL##*/}" in
  zsh)  RC="$HOME/.zshrc" ;;
  bash) RC="$HOME/.bashrc" ;;
  *)    RC="$HOME/.profile" ;;
esac

if grep -qF 'OSCP_DIR/shell.sh' "$RC" 2>/dev/null; then
  say "Shell integration already present in $RC."
else
  {
    echo ""
    echo "# OSCP Lab Tracker"
    echo "export OSCP_DIR=\"$OSCP_DIR\""
    echo 'source "$OSCP_DIR/shell.sh"'
  } >> "$RC"
  say "Added shell integration to $RC."
fi

# 5. tmux (for the background dashboard) ------------------------------------
if ! command -v tmux >/dev/null 2>&1; then
  warn "tmux not found — 'oscp-up' (background dashboard) needs it:  sudo apt install tmux"
fi

# Done ----------------------------------------------------------------------
cat <<EOF

$(say "Setup complete.")

  1. Reload your shell:   source $RC
     (or just open a new terminal)

  Now available from anywhere:
    oscp            the CLI        — add / ls / show / port / cred / note / flag / archive / clear / push
    target <box>    load \$ip \$domain \$user \$pass into your current shell
    oscp-up         start the dashboard at http://127.0.0.1:8777 (runs in background)
    oscp-down       stop the dashboard
    oscp-log        watch the dashboard's output

  Try it:
    oscp add Sauna --ip 10.10.11.42 --os Windows
    target sauna && nmap -sCV \$ip
EOF
