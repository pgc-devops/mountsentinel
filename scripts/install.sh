#!/usr/bin/env bash
# Idempotent install script for mountsentinel.
# Run as root after building the binary (make build).
set -euo pipefail

BINARY="${1:-./mountsentinel}"
INSTALL_BIN="/usr/local/bin/mountsentinel"
SERVICE_SRC="./dist/mountsentinel.service"
SERVICE_DST="/etc/systemd/system/mountsentinel.service"
CONFIG_SRC="./dist/mountsentinel.yml.example"
CONFIG_DST="/etc/mountsentinel.yml"
ZABBIX_CONF_SRC="./dist/zabbix/mountsentinel.conf"
ZABBIX_CONF_DST="/etc/zabbix/zabbix_agentd.d/mountsentinel.conf"
STATE_DIR="/var/lib/mountsentinel"
USER="mountsentinel"

if [[ $EUID -ne 0 ]]; then
    echo "error: must run as root" >&2
    exit 1
fi

echo "==> Creating mountsentinel system user"
if ! id -u "$USER" &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$USER"
    echo "    created user $USER"
else
    echo "    user $USER already exists"
fi

echo "==> Creating state directory"
install -d -m 750 -o "$USER" -g "$USER" "$STATE_DIR"

echo "==> Installing binary"
install -m 755 -o root -g root "$BINARY" "$INSTALL_BIN"
echo "    installed $INSTALL_BIN"

echo "==> Installing systemd unit"
install -m 644 -o root -g root "$SERVICE_SRC" "$SERVICE_DST"
systemctl daemon-reload

echo "==> Installing config (skip if exists)"
if [[ ! -f "$CONFIG_DST" ]]; then
    install -m 640 -o root -g "$USER" "$CONFIG_SRC" "$CONFIG_DST"
    echo "    installed $CONFIG_DST — edit before starting"
else
    echo "    $CONFIG_DST already exists, skipping"
fi

echo "==> Installing polkit rule (requires polkit with JavaScript rules support)"
# new polkit >= 121 (Debian 12+, Ubuntu 24.04+) always has JS rules.
# old polkit >= 0.106 on RHEL-family is compiled with mozjs by Red Hat.
# Ubuntu <= 22.04 and Debian <= 11 ship polkit 0.105 — silently ignores .rules files.
POLKIT_RULES_DIR="/etc/polkit-1/rules.d"
POLKIT_RULE_SRC="./dist/50-mountsentinel.rules"
_polkit_supports_js=false
if [[ -d "$POLKIT_RULES_DIR" ]] && command -v pkaction >/dev/null 2>&1; then
    _pver=$(pkaction --version 2>/dev/null | awk '{print $NF}')
    _pmajor=$(printf '%s' "$_pver" | cut -d. -f1)
    _pminor=$(printf '%s' "$_pver" | cut -d. -f2)
    if (( _pmajor >= 121 )) 2>/dev/null; then
        _polkit_supports_js=true
    elif (( _pmajor == 0 && ${_pminor:-0} >= 106 )) 2>/dev/null && [[ -f /etc/redhat-release ]]; then
        _polkit_supports_js=true
    fi
fi
if [[ "$_polkit_supports_js" == "true" ]] && [[ -f "$POLKIT_RULE_SRC" ]]; then
    install -m 644 -o root -g root "$POLKIT_RULE_SRC" "$POLKIT_RULES_DIR/50-mountsentinel.rules"
    systemctl reload polkit 2>/dev/null || true
    echo "    installed — systemctl reboot now authorised for mountsentinel user"
else
    echo "    skipped (polkit JS rules not supported; reboot -f fallback via CAP_SYS_BOOT)"
fi

echo "==> Installing Zabbix UserParameter config (skip if zabbix_agentd.d not found)"
if [[ -d "$(dirname "$ZABBIX_CONF_DST")" ]]; then
    install -m 644 -o root -g root "$ZABBIX_CONF_SRC" "$ZABBIX_CONF_DST"
    echo "    installed $ZABBIX_CONF_DST"
    # Restart whichever zabbix agent is present
    for svc in zabbix-agent2 zabbix-agent zabbix_agentd; do
        if systemctl is-active --quiet "$svc" 2>/dev/null; then
            systemctl restart "$svc"
            echo "    restarted $svc"
            break
        fi
    done
else
    echo "    zabbix_agentd.d not found, skipping Zabbix config"
fi

echo ""
echo "Done. Next steps:"
echo "  1. Edit $CONFIG_DST"
echo "  2. systemctl enable --now mountsentinel"
echo "  3. journalctl -fu mountsentinel"
