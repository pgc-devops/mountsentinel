#!/usr/bin/env bash
# Runs after package files are placed on disk.
set -euo pipefail

# Ensure state dir has correct permissions.
if [ -d /var/lib/mountsentinel ]; then
    chown root:root /var/lib/mountsentinel
    chmod 750 /var/lib/mountsentinel
fi

# Ensure config is only readable by root.
if [ -f /etc/mountsentinel.yml ]; then
    chown root:root /etc/mountsentinel.yml
    chmod 600 /etc/mountsentinel.yml
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

# Restart Zabbix agent if config was installed and agent is running.
if [ -f /etc/zabbix/zabbix_agentd.d/mountsentinel.conf ]; then
    for svc in zabbix-agent2 zabbix-agent zabbix_agentd; do
        if systemctl is-active --quiet "$svc" 2>/dev/null; then
            systemctl restart "$svc" ||:
            break
        fi
    done
fi
