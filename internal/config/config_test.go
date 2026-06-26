// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mountsentinel*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoad_Minimal(t *testing.T) {
	path := writeConfig(t, `
watch_mounts:
  - mountpoint: "/data"
    device: "/dev/sdb1"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Defaults should be applied.
	if cfg.Daemon.CheckInterval.Duration != 30*time.Second {
		t.Errorf("expected 30s check_interval default, got %v", cfg.Daemon.CheckInterval)
	}
	if cfg.Backoff.Multiplier != 2.0 {
		t.Errorf("expected multiplier 2.0, got %v", cfg.Backoff.Multiplier)
	}
	if cfg.State.Backend != "file" {
		t.Errorf("expected file backend default, got %v", cfg.State.Backend)
	}
}

func TestLoad_Override(t *testing.T) {
	path := writeConfig(t, `
daemon:
  check_interval: "1m"
  dry_run: true
  log_level: "debug"
watch_mounts:
  - mountpoint: "/data"
backoff:
  base_delay: "10m"
  multiplier: 3.0
  max_delay: "8h"
  window: "48h"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.CheckInterval.Duration != time.Minute {
		t.Errorf("expected 1m, got %v", cfg.Daemon.CheckInterval)
	}
	if !cfg.Daemon.DryRun {
		t.Error("expected dry_run=true")
	}
	if cfg.Backoff.Multiplier != 3.0 {
		t.Errorf("expected multiplier 3.0, got %v", cfg.Backoff.Multiplier)
	}
	if cfg.Backoff.BaseDelay.Duration != 10*time.Minute {
		t.Errorf("expected 10m base_delay, got %v", cfg.Backoff.BaseDelay)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yml"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeConfig(t, `daemon: {bad yaml [[[`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	path := writeConfig(t, `
watch_mounts:
  - mountpoint: "/data"
daemon:
  check_interval: "not-a-duration"
`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestValidate_ZeroCheckInterval(t *testing.T) {
	cfg := Defaults()
	cfg.Daemon.CheckInterval.Duration = 0
	cfg.WatchMounts = []WatchMount{{Mountpoint: "/data", Device: "/dev/sdb1"}}
	if err := cfg.validate(); err == nil {
		t.Error("expected validation error for zero check_interval")
	}
}

func TestValidate_MaxDelayLessThanBase(t *testing.T) {
	cfg := Defaults()
	cfg.WatchMounts = []WatchMount{{Mountpoint: "/data", Device: "/dev/sdb1"}}
	cfg.Backoff.MaxDelay.Duration = time.Minute
	cfg.Backoff.BaseDelay.Duration = 10 * time.Minute
	if err := cfg.validate(); err == nil {
		t.Error("expected validation error when max_delay < base_delay")
	}
}

func TestValidate_EmptyWatchMount(t *testing.T) {
	cfg := Defaults()
	cfg.WatchMounts = []WatchMount{{}} // no mountpoint or device
	if err := cfg.validate(); err == nil {
		t.Error("expected validation error for empty watch_mount entry")
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Daemon.CheckInterval.Duration <= 0 {
		t.Error("default check_interval must be positive")
	}
	if cfg.Backoff.Multiplier <= 0 {
		t.Error("default multiplier must be positive")
	}
	if cfg.State.Backend == "" {
		t.Error("default backend must be set")
	}
}

func TestDefaultExcludes(t *testing.T) {
	excl := DefaultExcludes()
	if len(excl) == 0 {
		t.Error("expected non-empty default excludes")
	}
	found := false
	for _, e := range excl {
		if e == "/proc" {
			found = true
		}
	}
	if !found {
		t.Error("expected /proc in default excludes")
	}
}
