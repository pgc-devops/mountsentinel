package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = dur
	return nil
}

type Config struct {
	Daemon      DaemonConfig   `yaml:"daemon"`
	WatchMounts []WatchMount   `yaml:"watch_mounts"`
	Reboot      RebootConfig   `yaml:"reboot"`
	Backoff     BackoffConfig  `yaml:"backoff"`
	Notify      NotifyConfig   `yaml:"notify"`
	State       StateConfig    `yaml:"state"`
	Zabbix      ZabbixConfig   `yaml:"zabbix"`
	Metrics     MetricsConfig  `yaml:"metrics"`
}

type DaemonConfig struct {
	CheckInterval Duration `yaml:"check_interval"`
	DryRun        bool     `yaml:"dry_run"`
	LogLevel      string   `yaml:"log_level"`
	ProcMountsPath string  `yaml:"proc_mounts_path"` // override for testing
}

type WatchMount struct {
	Mountpoint string   `yaml:"mountpoint"` // "*" for wildcard
	Device     string   `yaml:"device"`
	Label      string   `yaml:"label"`
	Exclude    []string `yaml:"exclude"` // used when mountpoint is "*"
}

type RebootConfig struct {
	Delay          Duration     `yaml:"delay"`
	PreRebootHooks []HookConfig `yaml:"pre_reboot_hooks"`
}

type HookConfig struct {
	Cmd     string   `yaml:"cmd"`
	Args    []string `yaml:"args"`
	Timeout Duration `yaml:"timeout"`
}

type BackoffConfig struct {
	Window     Duration `yaml:"window"`
	BaseDelay  Duration `yaml:"base_delay"`
	Multiplier float64  `yaml:"multiplier"`
	MaxDelay   Duration `yaml:"max_delay"`
	Jitter     Duration `yaml:"jitter"`
}

type NotifyConfig struct {
	Webhook *WebhookConfig `yaml:"webhook"`
}

type WebhookConfig struct {
	URL          string            `yaml:"url"`
	Method       string            `yaml:"method"`
	Headers      map[string]string `yaml:"headers"`
	BodyTemplate string            `yaml:"body_template"`
	Timeout      Duration          `yaml:"timeout"`
}

type StateConfig struct {
	Backend          string   `yaml:"backend"`           // file|tmpfs|xenstore|memory|remote
	FilePath         string   `yaml:"file_path"`
	RemoteURL        string   `yaml:"remote_url"`
	FallbackBackends []string `yaml:"fallback_backends"`
}

type ZabbixConfig struct {
	Enabled   bool   `yaml:"enabled"`
	StateFile string `yaml:"state_file"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

var defaultExcludes = []string{
	"/proc", "/sys", "/dev", "/run", "/tmp",
	"/proc/sys/fs/binfmt_misc",
}

func Defaults() *Config {
	return &Config{
		Daemon: DaemonConfig{
			CheckInterval:  Duration{30 * time.Second},
			LogLevel:       "info",
			ProcMountsPath: "/proc/mounts",
		},
		Reboot: RebootConfig{
			Delay: Duration{5 * time.Minute},
		},
		Backoff: BackoffConfig{
			Window:     Duration{24 * time.Hour},
			BaseDelay:  Duration{5 * time.Minute},
			Multiplier: 2.0,
			MaxDelay:   Duration{4 * time.Hour},
			Jitter:     Duration{30 * time.Second},
		},
		State: StateConfig{
			Backend:          "file",
			FilePath:         "/var/lib/mountsentinel/state.json",
			FallbackBackends: []string{"tmpfs", "memory"},
		},
		Zabbix: ZabbixConfig{
			StateFile: "/run/mountsentinel/zabbix.json",
		},
		Metrics: MetricsConfig{
			Addr: ":9101",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Daemon.CheckInterval.Duration <= 0 {
		return fmt.Errorf("daemon.check_interval must be positive")
	}
	if c.Backoff.Multiplier <= 0 {
		return fmt.Errorf("backoff.multiplier must be > 0")
	}
	if c.Backoff.MaxDelay.Duration < c.Backoff.BaseDelay.Duration {
		return fmt.Errorf("backoff.max_delay must be >= backoff.base_delay")
	}
	for i, wm := range c.WatchMounts {
		if wm.Mountpoint == "" && wm.Device == "" {
			return fmt.Errorf("watch_mounts[%d]: mountpoint or device required", i)
		}
	}
	return nil
}

// DefaultExcludes returns the built-in excluded mountpoints for wildcard watches.
func DefaultExcludes() []string { return append([]string{}, defaultExcludes...) }
