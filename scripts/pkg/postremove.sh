#!/usr/bin/env bash
# Runs after package files are removed.
set -euo pipefail

systemctl daemon-reload 2>/dev/null ||:

# Remove state dir on full purge (data is intentionally kept on plain remove).
# deb: $1 = "purge"; rpm: $1 = "0"
case "${1:-}" in
    purge|0)
        rm -rf /var/lib/mountsentinel
        ;;
esac
