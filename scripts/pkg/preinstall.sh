#!/usr/bin/env bash
# Runs before package files are placed on disk.
set -euo pipefail

if ! id -u mountsentinel &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin mountsentinel
fi
