#!/usr/bin/env bash
# Runs after package files are removed.
set -euo pipefail

systemctl daemon-reload 2>/dev/null ||:

# Remove user only on full purge, not upgrade.
# deb: $1 = "purge"; rpm: $1 = "0"
case "${1:-}" in
    purge|0)
        if id -u mountsentinel &>/dev/null; then
            userdel mountsentinel 2>/dev/null ||:
        fi
        # Remove state dir on purge (data is intentionally kept on plain remove).
        rm -rf /var/lib/mountsentinel
        ;;
esac
