# mountsentinel

Linux filesystem read-only monitor for iSCSI SAN environments. Detects when a VM's filesystem is remounted read-only (due to SAN failover or network interruption), waits a configurable delay, then reboots the VM. Implements exponential backoff to prevent reboot storms when the underlying issue persists.

Designed to run on Linux VMs under XenServer or XCP-ng with iSCSI SAN storage.

---

## How It Works

1. Reads `/proc/mounts` on every check interval
2. Detects watched mounts with the `ro` option flag
3. Waits a backoff-calculated delay (default 5 min for first occurrence)
4. Runs pre-reboot hooks (notifications, log flush, etc.)
5. Executes `systemctl reboot`
6. Persists reboot history so subsequent incidents have longer delays

If a mount returns to `rw` before the delay expires, the reboot is cancelled and a recovery notification is sent.

---

## Installation

### Prerequisites

- Go 1.22+
- Linux (systemd)
- XenServer / XCP-ng (optional: for xenstore backend)

---

### Option 1 — Install from .deb (Debian / Ubuntu)

```bash
# Download the .deb from the releases page, then:
sudo dpkg -i mountsentinel_<version>_amd64.deb

# Edit config
sudo nano /etc/mountsentinel.yml

sudo systemctl start mountsentinel
```

On upgrade:
```bash
sudo dpkg -i mountsentinel_<new_version>_amd64.deb   # restarts daemon automatically
```

On removal:
```bash
sudo apt remove mountsentinel          # stops service, keeps config + state
sudo apt purge mountsentinel           # also removes user and /var/lib/mountsentinel
```

---

### Option 2 — Install from .rpm (RHEL / Rocky / AlmaLinux / CentOS)

```bash
sudo rpm -ivh mountsentinel-<version>.x86_64.rpm

# Edit config
sudo nano /etc/mountsentinel.yml

sudo systemctl start mountsentinel
```

On upgrade:
```bash
sudo rpm -Uvh mountsentinel-<new_version>.x86_64.rpm
```

On removal:
```bash
sudo rpm -e mountsentinel              # stops service, keeps config + state
```

---

### Option 3 — Manual install (any distro)

```bash
git clone <repo>
cd projects/systemhealthagent

go mod tidy
make build-static

# Install (as root)
sudo bash scripts/install.sh ./mountsentinel
```

The install script:
- Creates `mountsentinel` system user
- Creates `/var/lib/mountsentinel/` state directory
- Installs binary to `/usr/local/bin/mountsentinel`
- Installs systemd unit to `/etc/systemd/system/mountsentinel.service`
- Installs example config to `/etc/mountsentinel.yml` (if not present)
- Installs Zabbix UserParameter config (if `/etc/zabbix/zabbix_agentd.d/` exists)

---

### Start

```bash
# Edit config first
sudo nano /etc/mountsentinel.yml

sudo systemctl enable --now mountsentinel
sudo journalctl -fu mountsentinel
```

---

## Building Packages

Packages are built with [nFPM](https://nfpm.goreleaser.com/). Requires Go 1.22+.

### Setup

```bash
# Install nfpm (once)
make nfpm-install
```

### Build

```bash
# Both .deb and .rpm
make packages

# Individual formats
make deb    # → dist/mountsentinel_<version>_amd64.deb
make rpm    # → dist/mountsentinel-<version>.x86_64.rpm
make apk    # → dist/mountsentinel_<version>_x86_64.apk  (Alpine)
```

Version is taken from `git describe --tags`. Tag a release before building:

```bash
git tag v1.0.0
make packages
```

To build with an explicit version:

```bash
VERSION=1.0.0 make packages
```

### Package contents

| Path | Notes |
|---|---|
| `/usr/local/bin/mountsentinel` | Static binary |
| `/lib/systemd/system/mountsentinel.service` | systemd unit |
| `/etc/mountsentinel.yml` | Default config (`config\|noreplace` — not overwritten on upgrade) |
| `/etc/zabbix/zabbix_agentd.d/mountsentinel.conf` | Zabbix UserParameter config |
| `/var/lib/mountsentinel/` | State directory (owned by `mountsentinel` user) |

### Package lifecycle

| Event | Behaviour |
|---|---|
| Fresh install | Creates user, enables service, prints start instructions |
| Upgrade | Restarts daemon if running; does not touch config |
| Remove | Stops and disables service; keeps config and state |
| Purge / `rpm -e` | Also deletes `mountsentinel` user and `/var/lib/mountsentinel` |

---

## Configuration

Config file defaults to `/etc/mountsentinel.yml`. Override with `--config`.

```yaml
daemon:
  check_interval: "30s"   # how often to poll /proc/mounts
  dry_run: false           # log reboot decisions without executing
  log_level: "info"        # info | debug | verbose

watch_mounts:
  - mountpoint: "/data"
    device: "/dev/sdb1"
    label: "iscsi-data"
  # wildcard: watch all mounts (minus exclusions)
  # - mountpoint: "*"
  #   exclude: ["/proc", "/sys", "/dev", "/run"]

reboot:
  delay: "5m"              # wait before rebooting after detection
  pre_reboot_hooks: []     # commands to run before reboot

backoff:
  window: "24h"            # rolling window for reboot history
  base_delay: "5m"         # first-incident delay
  multiplier: 2.0          # each repeat doubles the delay
  max_delay: "4h"          # cap; when reached, auto-reboot stops
  jitter: "30s"            # random jitter to prevent thundering herd

state:
  backend: "file"          # file | tmpfs | xenstore | memory | remote
  file_path: "/var/lib/mountsentinel/state.json"
  fallback_backends: ["tmpfs", "memory"]

notify:
  webhook:
    url: "https://hooks.slack.com/..."
    body_template: |
      {"text": "{{.Hostname}} mount {{.Mountpoint}} → {{.Event}}"}

zabbix:
  enabled: false
  state_file: "/run/mountsentinel/zabbix.json"

metrics:
  enabled: false
  addr: ":9101"
```

See `dist/mountsentinel.yml.example` for full annotated reference.

### State Backends

| Backend | Survives Reboot | Notes |
|---|---|---|
| `file` | Yes | Default. Risk: if `/var/lib` is on a watched mount |
| `tmpfs` | No | Recommended for iSCSI. Always writable (RAM). Resets cleanly after reboot |
| `xenstore` | No | XCP-ng/XenServer only. Uses `xenstore-write` CLI |
| `memory` | No | In-process only. Lost on daemon crash |
| `remote` | Yes | HTTP PUT/GET to configurable URL |

`fallback_backends` tries alternatives if the primary backend write fails:
```yaml
state:
  backend: "file"
  fallback_backends: ["tmpfs", "memory"]
```

---

## CLI Reference

### Daemon mode (default)

```bash
mountsentinel [--config /etc/mountsentinel.yml] [--verbose] [--debug]
```

### Status

```bash
# Table output (exits 2 if any mount is degraded — scriptable)
mountsentinel status

# Filter by mount
mountsentinel status --mount /data

# Zabbix LLD discovery JSON
mountsentinel status --format=zabbix-discovery

# Single item value (for Zabbix UserParameter)
mountsentinel status --mount /data --key state --format=value
mountsentinel status --mount /data --key reboot_count --format=value
```

Exit codes for `status`:
- `0` — all mounts healthy
- `2` — one or more mounts degraded (DETECTED / SUPPRESSED)

### Reset

Clears the SUPPRESSED state when `max_delay` has been reached. Requires operator action.

```bash
mountsentinel reset --mount /data
# or by device
mountsentinel reset --mount /dev/sdb1
```

### Reload config

```bash
systemctl reload mountsentinel
# or
kill -HUP $(pidof mountsentinel)
```

---

## Backoff

Delays between reboots grow exponentially over a rolling window:

```
delay = base_delay × multiplier^(reboots_in_window)
delay = min(delay, max_delay)
delay += rand(0, jitter)
```

With defaults (`base=5m, mult=2, max=4h`):

| Reboot # in window | Delay |
|---|---|
| 1st | 5 min |
| 2nd | 10 min |
| 3rd | 20 min |
| 4th | 40 min |
| 5th | 80 min |
| 6th+ | 240 min (capped) → **SUPPRESSED** |

When SUPPRESSED: operator must run `mountsentinel reset --mount <mp>` to re-enable auto-reboot.

---

## Zabbix Integration

mountsentinel writes `/run/mountsentinel/zabbix.json` (tmpfs, always writable) on every state change. The local Zabbix agent reads this via `UserParameter` scripts and forwards to the Zabbix server.

### Setup

1. Enable in config:
   ```yaml
   zabbix:
     enabled: true
     state_file: "/run/mountsentinel/zabbix.json"
   ```

2. Install agent config (done automatically by `scripts/install.sh`):
   ```bash
   sudo cp dist/zabbix/mountsentinel.conf /etc/zabbix/zabbix_agentd.d/
   sudo systemctl restart zabbix-agent
   ```

3. In Zabbix UI:
   - Create **Discovery Rule**: key `mountsentinel.discovery` — auto-creates items per mount
   - Create **Item Prototypes** using `{#MOUNT}` macro:
     - `mountsentinel.state[{#MOUNT}]` — HEALTHY / DETECTED / SUPPRESSED / REBOOTING
     - `mountsentinel.reboot_count[{#MOUNT}]` — integer counter
     - `mountsentinel.last_event[{#MOUNT}]` — ISO8601 timestamp
     - `mountsentinel.suppressed[{#MOUNT}]` — 0 or 1
   - Create **Trigger**: alert when `mountsentinel.state[*] <> "HEALTHY"`

### Architecture

```
mountsentinel daemon
    │  writes on each state change (atomic rename)
    ▼
/run/mountsentinel/zabbix.json   (tmpfs — always writable)
    ▲
    │  UserParameter reads on Zabbix server poll
zabbix_agentd
    │  forwards
    ▼
Zabbix Server
```

No direct mountsentinel → Zabbix server connection. Agent handles transport.

---

## Prometheus Metrics

Enable in config:
```yaml
metrics:
  enabled: true
  addr: ":9101"
```

Available metrics at `http://localhost:9101/metrics`:

| Metric | Type | Description |
|---|---|---|
| `mountsentinel_mount_state` | gauge | 0=HEALTHY 1=DETECTED 2=SUPPRESSED 3=REBOOTING |
| `mountsentinel_mount_reboot_total` | counter | Lifetime reboot count per mount |
| `mountsentinel_mount_suppressed` | gauge | 1 if suppressed (max backoff reached) |

Health endpoint: `http://localhost:9101/healthz`

---

## Logs

All logs are structured JSON to stdout → captured by journald.

```bash
journalctl -fu mountsentinel | jq .
```

Fields: `ts`, `level`, `event`, `mount`, `device`, `backoff_delay`, `reboot_at`, `dry_run`.

---

## Development

```bash
# Run tests
make test

# Run with verbose logging against a test config
./mountsentinel --config testdata/mountsentinel-test.yml --verbose

# Dry run (safe for staging)
./mountsentinel --config /etc/mountsentinel.yml
# with dry_run: true in config, no actual reboots will occur
```

### Testing read-only detection

To test without a real SAN failure, temporarily remount a filesystem read-only:
```bash
sudo mount -o remount,ro /data
# mountsentinel will detect within check_interval
sudo mount -o remount,rw /data
# mountsentinel logs "mount_recovered"
```

---

## Security

- Runs as unprivileged `mountsentinel` system user
- Only `CAP_SYS_BOOT` capability granted (required for reboot syscall)
- Full systemd sandboxing: `ProtectSystem=strict`, `PrivateTmp`, `MemoryDenyWriteExecute`
- Config file readable by `mountsentinel` group only (mode 640)
- State file in `/var/lib/mountsentinel/` (mode 750, owned by mountsentinel)
