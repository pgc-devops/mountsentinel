// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package backoff

import (
	"testing"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
)

var testCfg = config.BackoffConfig{
	Window:     config.Duration{Duration: 24 * time.Hour},
	BaseDelay:  config.Duration{Duration: 5 * time.Minute},
	Multiplier: 2.0,
	MaxDelay:   config.Duration{Duration: 4 * time.Hour},
	Jitter:     config.Duration{Duration: 0}, // no jitter for deterministic tests
}

func TestCalculate_NoHistory(t *testing.T) {
	d := Calculate(nil, testCfg)
	if d != 5*time.Minute {
		t.Errorf("expected 5m, got %v", d)
	}
}

func TestCalculate_OneReboot(t *testing.T) {
	reboots := []time.Time{time.Now().Add(-1 * time.Hour)}
	d := Calculate(reboots, testCfg)
	// count=1, delay = base * 2^1 = 10m
	if d != 10*time.Minute {
		t.Errorf("expected 10m, got %v", d)
	}
}

func TestCalculate_TwoReboots(t *testing.T) {
	reboots := []time.Time{
		time.Now().Add(-2 * time.Hour),
		time.Now().Add(-1 * time.Hour),
	}
	d := Calculate(reboots, testCfg)
	// count=2, delay = base * 2^2 = 20m
	if d != 20*time.Minute {
		t.Errorf("expected 20m, got %v", d)
	}
}

func TestCalculate_MaxDelayCapped(t *testing.T) {
	reboots := make([]time.Time, 10)
	for i := range reboots {
		reboots[i] = time.Now().Add(-time.Duration(i+1) * time.Minute)
	}
	d := Calculate(reboots, testCfg)
	if d != 4*time.Hour {
		t.Errorf("expected 4h (max), got %v", d)
	}
}

func TestCalculate_OldRebootsIgnored(t *testing.T) {
	reboots := []time.Time{
		time.Now().Add(-48 * time.Hour), // outside 24h window
	}
	d := Calculate(reboots, testCfg)
	// count=0 (old reboot ignored), so base delay
	if d != 5*time.Minute {
		t.Errorf("expected 5m (old reboot ignored), got %v", d)
	}
}

func TestSuppressed(t *testing.T) {
	// Build enough reboots to hit max_delay.
	reboots := make([]time.Time, 10)
	for i := range reboots {
		reboots[i] = time.Now().Add(-time.Duration(i+1) * time.Minute)
	}
	if !Suppressed(reboots, testCfg) {
		t.Error("expected suppressed=true at max backoff")
	}
	if Suppressed(nil, testCfg) {
		t.Error("expected suppressed=false with no history")
	}
}

func TestPruneOld(t *testing.T) {
	now := time.Now()
	reboots := []time.Time{
		now.Add(-50 * time.Hour), // older than 2×24h window
		now.Add(-10 * time.Hour),
		now.Add(-1 * time.Hour),
	}
	pruned := PruneOld(reboots, 24*time.Hour)
	if len(pruned) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(pruned))
	}
}
