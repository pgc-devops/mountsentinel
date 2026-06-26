#!/usr/bin/env bash
# Runs after package files are removed.
set -euo pipefail

systemctl daemon-reload 2>/dev/null ||:

# Remove state dir and polkit rule on full purge (kept on plain remove).
# deb: $1 = "purge"; rpm: $1 = "0"
case "${1:-}" in
    purge|0)
        rm -rf /var/lib/mountsentinel
        rm -f /etc/polkit-1/rules.d/50-mountsentinel.rules
        systemctl reload polkit 2>/dev/null ||:
        ;;
esac
