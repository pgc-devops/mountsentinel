#!/usr/bin/env bash
# Runs before package files are removed.
# On upgrade this runs before the new package installs — only stop on full removal.
set -euo pipefail

# $1 = "remove" (deb full removal) or "0" (rpm full removal)
# $1 = "upgrade" (deb upgrade) or "1" (rpm upgrade) — skip stop
IS_UPGRADE=false
case "${1:-}" in
    upgrade|1) IS_UPGRADE=true ;;
esac

if [ "$IS_UPGRADE" = "false" ]; then
    if systemctl is-active --quiet mountsentinel 2>/dev/null; then
        systemctl stop mountsentinel ||:
    fi
    systemctl disable mountsentinel 2>/dev/null ||:
fi
