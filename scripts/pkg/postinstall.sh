#!/usr/bin/env bash
# Runs after package files are placed on disk.
set -euo pipefail

# Ensure state dir is owned by the service user.
if [ -d /var/lib/mountsentinel ]; then
    chown mountsentinel:mountsentinel /var/lib/mountsentinel
    chmod 750 /var/lib/mountsentinel
fi

# Ensure config is readable by the mountsentinel group (service user) but not world.
if [ -f /etc/mountsentinel.yml ]; then
    chown root:mountsentinel /etc/mountsentinel.yml
    chmod 640 /etc/mountsentinel.yml
fi

systemctl daemon-reload

# On fresh install: enable but don't start (operator must configure first).
# On upgrade: restart if was already running.
if [ "$1" = "configure" ] || [ "$1" = "1" ]; then
    # Fresh install (deb passes "configure", rpm passes "1").
    systemctl enable mountsentinel ||:
    echo ""
    echo "mountsentinel installed. Edit /etc/mountsentinel.yml then:"
    echo "  systemctl start mountsentinel"
elif [ "$1" = "2" ] || [ -n "${DPKG_MAINTSCRIPT_PACKAGE:-}" ]; then
    # Upgrade.
    if systemctl is-active --quiet mountsentinel 2>/dev/null; then
        systemctl restart mountsentinel
    fi
fi

# Install polkit rule only when polkit supports JavaScript rules:
#   new polkit >= 121 (Debian 12+, Ubuntu 24.04+) — always has JS rules
#   old polkit >= 0.106 on RHEL-family (compiled with mozjs by Red Hat)
# Ubuntu <= 22.04 and Debian <= 11 ship polkit 0.105 which ignores .rules files.
POLKIT_RULES_DIR="/etc/polkit-1/rules.d"
POLKIT_RULE="$POLKIT_RULES_DIR/50-mountsentinel.rules"
POLKIT_RULE_SRC="/usr/share/mountsentinel/50-mountsentinel.rules"
_polkit_supports_js=false
if [ -d "$POLKIT_RULES_DIR" ] && command -v pkaction >/dev/null 2>&1; then
    _pver=$(pkaction --version 2>/dev/null | awk '{print $NF}')
    _pmajor=$(printf '%s' "$_pver" | cut -d. -f1)
    _pminor=$(printf '%s' "$_pver" | cut -d. -f2)
    if [ "${_pmajor:-0}" -ge 121 ] 2>/dev/null; then
        _polkit_supports_js=true
    elif [ "${_pmajor:-0}" -eq 0 ] && [ "${_pminor:-0}" -ge 106 ] 2>/dev/null && [ -f /etc/redhat-release ]; then
        _polkit_supports_js=true
    fi
fi
if [ "$_polkit_supports_js" = "true" ] && [ -f "$POLKIT_RULE_SRC" ]; then
    install -m 644 -o root -g root "$POLKIT_RULE_SRC" "$POLKIT_RULE"
    systemctl reload polkit 2>/dev/null ||:
fi

# Restart Zabbix agent if config was installed and agent is running.
if [ -f /etc/zabbix/zabbix_agentd.d/mountsentinel.conf ]; then
    for svc in zabbix-agent2 zabbix-agent zabbix_agentd; do
        if systemctl is-active --quiet "$svc" 2>/dev/null; then
            systemctl restart "$svc" ||:
            break
        fi
    done
fi
