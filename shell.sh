# OSCP Lab Tracker — shell integration (bash / zsh)
# ---------------------------------------------------------------------------
# Source this once from your ~/.bashrc or ~/.zshrc:
#
#     export OSCP_DIR="$HOME/oscp-lab-tracker"     # path to this repo
#     source "$OSCP_DIR/shell.sh"
#
# Then, in any lab shell:
#
#     target sauna        # focus a box: sets $ip $domain $rhost $box $user $pass
#     nmap -sCV $ip
#     smbclient -L //$ip -U "$user%$pass"
#     curl http://$domain/
#     target              # re-load the last focused box in a new terminal
#
# `oscp` is also exposed here so you can run it from anywhere:
#     oscp add sauna --ip 10.10.11.42 && oscp flag sauna root
# ---------------------------------------------------------------------------

export OSCP_DIR="${OSCP_DIR:-$HOME/oscp-lab-tracker}"

# Call the compiled binary. (Falls back to `go run` if you haven't built it yet.)
oscp() {
  if [ -x "$OSCP_DIR/oscp" ]; then
    "$OSCP_DIR/oscp" "$@"
  else
    ( cd "$OSCP_DIR" && go run oscp.go "$@" )
  fi
}

# Load a host's info into the CURRENT shell's environment.
# With no argument, reuses the last focused target.
target() { eval "$(oscp env ${1:+"$1"})"; }

# Tab-completion for `target`, from the host names in the tracker.
_oscp_targets() { oscp ls 2>/dev/null | awk 'NR>1 && NF && $0 !~ /host\(s\)/ {print $1}'; }
if [ -n "$ZSH_VERSION" ]; then
  compdef '_values targets $(_oscp_targets)' target 2>/dev/null
elif [ -n "$BASH_VERSION" ]; then
  complete -W "$(_oscp_targets 2>/dev/null)" target 2>/dev/null
fi
