# mountsentinel Ansible Role

Installs and configures [mountsentinel](https://github.com/pgc-devops/mountsentinel) — a filesystem
read-only monitor and automatic recovery daemon for iSCSI SAN environments.

Downloads the release package directly from GitHub, generates the config from a Jinja2 template,
and manages the systemd service.

---

## Requirements

- **Ansible** ≥ 2.12
- **Target OS**: see [Supported Platforms](#supported-platforms)
- **Privilege**: tasks run as root (`become: true` in your play)
- **Network**: target hosts must be able to reach `github.com` to download releases

### Supported Platforms

| Distribution | Min version | systemd |
|---|---|---|
| Ubuntu | 18.04 LTS | 237 |
| Debian | 9 (Stretch) | 231 |
| RHEL / CentOS Stream | 8 | 239 |
| Rocky Linux | 8 | 239 |
| AlmaLinux | 8 | 239 |

RHEL/CentOS 7 is **not supported** — systemd 219 predates `AmbientCapabilities`.

---

## Role Variables

All variables are prefixed `mountsentinel_` to avoid collisions in a collection context.

### Installation

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_enabled` | `true` | Set `false` to skip the entire role |
| `mountsentinel_version` | `"latest"` | Release to install. `"latest"` resolves via the GitHub API. Pin e.g. `"1.2.3"` for reproducible deployments |
| `mountsentinel_github_repo` | `"pgc-devops/mountsentinel"` | GitHub `org/repo` for release downloads |

### Daemon

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_check_interval` | `"30s"` | How often to poll `/proc/mounts` |
| `mountsentinel_log_level` | `"info"` | `info` \| `debug` \| `verbose` |
| `mountsentinel_dry_run` | `false` | Log reboot decisions without executing — safe for staging |
| `mountsentinel_proc_mounts_path` | `"/proc/1/mounts"` | Mount table path; override in containers or tests |

### Reboot

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_reboot_enabled` | `true` | Enable automatic reboot on detection |
| `mountsentinel_reboot_delay` | `"5m"` | Wait this long after detection before rebooting |
| `mountsentinel_pre_reboot_hooks` | `[]` | Commands to run before rebooting (see example below) |

### Watch Mounts

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_watch_mounts` | `[]` | List of mount entries to watch (required — at least one entry) |

Each entry supports:

```yaml
mountsentinel_watch_mounts:
  - mountpoint: "/data"       # required (or use "*" for wildcard)
    device: "/dev/sdb1"       # optional — match by device
    label: "iscsi-data"       # optional — appears in logs and notifications
  - mountpoint: "*"
    exclude:                  # used only with wildcard mountpoint
      - "/proc"
      - "/sys"
      - "/dev"
      - "/run"
      - "/tmp"
```

### Backoff

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_backoff_window` | `"24h"` | Rolling window for counting prior reboots |
| `mountsentinel_backoff_base_delay` | `"5m"` | Delay before first reboot in a window |
| `mountsentinel_backoff_multiplier` | `2.0` | Delay multiplier per subsequent reboot |
| `mountsentinel_backoff_max_delay` | `"4h"` | Cap; when reached, auto-reboot is suppressed |
| `mountsentinel_backoff_jitter` | `"30s"` | Random jitter to prevent thundering herd |

### State Backend

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_state_backend` | `"file"` | `file` \| `tmpfs` \| `xenstore` \| `memory` \| `remote` |
| `mountsentinel_state_file_path` | `"/var/lib/mountsentinel/state.json"` | Used when backend is `file` |
| `mountsentinel_state_remote_url` | `""` | Used when backend is `remote` |
| `mountsentinel_state_fallback_backends` | `["tmpfs", "memory"]` | Tried in order if primary write fails |

> For iSCSI environments, use `backend: tmpfs` — it is always writable even when block storage is read-only.

### Notifications (Webhook)

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_notify_webhook_url` | `""` | Webhook URL; leave empty to disable |
| `mountsentinel_notify_webhook_method` | `"POST"` | HTTP method |
| `mountsentinel_notify_webhook_timeout` | `"10s"` | Per-request timeout |
| `mountsentinel_notify_webhook_headers` | `{}` | Extra HTTP headers (map) |
| `mountsentinel_notify_webhook_body_template` | `""` | Go `text/template` body |

Template variables: `.Hostname` `.Mountpoint` `.Event` `.RebootCount`

### Zabbix

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_zabbix_enabled` | `false` | Write Zabbix state JSON on each state change |
| `mountsentinel_zabbix_state_file` | `"/run/mountsentinel/zabbix.json"` | State file path (must be on tmpfs) |

### Prometheus Metrics

| Variable | Default | Description |
|---|---|---|
| `mountsentinel_metrics_enabled` | `false` | Expose Prometheus metrics endpoint |
| `mountsentinel_metrics_addr` | `":9101"` | Listen address |

---

## Dependencies

None.

---

## Example Playbooks

### Minimal — watch specific mounts, reboot enabled

```yaml
- hosts: iscsi_vms
  become: true
  roles:
    - role: mountsentinel
      vars:
        mountsentinel_watch_mounts:
          - mountpoint: "/data"
            label: "iscsi-data"
          - mountpoint: "/var/lib/postgresql"
            label: "iscsi-pg"
```

### iSCSI environment — tmpfs state, Slack webhook, pre-reboot hook

```yaml
- hosts: xen_vms
  become: true
  roles:
    - role: mountsentinel
      vars:
        mountsentinel_version: "1.2.0"          # pin for reproducibility
        mountsentinel_state_backend: "tmpfs"    # always writable on iSCSI hosts

        mountsentinel_watch_mounts:
          - mountpoint: "*"
            exclude:
              - "/proc"
              - "/sys"
              - "/dev"
              - "/run"
              - "/tmp"

        mountsentinel_reboot_delay: "3m"
        mountsentinel_pre_reboot_hooks:
          - cmd: "/usr/local/bin/drain-connections.sh"
            timeout: "60s"

        mountsentinel_notify_webhook_url: "https://hooks.slack.com/services/T00/B00/xxx"
        mountsentinel_notify_webhook_body_template: >-
          {"text": ":warning: *{{ '{{' }}.Hostname{{ '}}' }}* `{{ '{{' }}.Mountpoint{{ '}}' }}` → *{{ '{{' }}.Event{{ '}}' }}*
          (reboots: {{ '{{' }}.RebootCount{{ '}}' }})"}

        mountsentinel_backoff_base_delay: "3m"
        mountsentinel_backoff_max_delay: "2h"
```

### Staging — dry run, no actual reboots

```yaml
- hosts: staging
  become: true
  roles:
    - role: mountsentinel
      vars:
        mountsentinel_dry_run: true
        mountsentinel_watch_mounts:
          - mountpoint: "/data"
```

### Disable without removing from play

```yaml
- hosts: all
  become: true
  roles:
    - role: mountsentinel
      vars:
        mountsentinel_enabled: false
```

### Used inside a collection

```yaml
- hosts: san_hosts
  become: true
  roles:
    - role: myorg.myinfra.mountsentinel
      vars:
        mountsentinel_watch_mounts:
          - mountpoint: "/san"
```

---

## License

MIT
