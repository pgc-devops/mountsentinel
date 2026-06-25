package backoff

import (
	"math"
	"math/rand"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
)

// Calculate returns the delay before the next reboot given the history of prior
// reboots and the configured backoff parameters.
func Calculate(reboots []time.Time, cfg config.BackoffConfig) time.Duration {
	now := time.Now()
	count := 0
	for _, t := range reboots {
		if now.Sub(t) <= cfg.Window.Duration {
			count++
		}
	}
	if count == 0 {
		return addJitter(cfg.BaseDelay.Duration, cfg.Jitter.Duration)
	}
	delay := float64(cfg.BaseDelay.Duration) * math.Pow(cfg.Multiplier, float64(count))
	if delay > float64(cfg.MaxDelay.Duration) {
		delay = float64(cfg.MaxDelay.Duration)
	}
	return addJitter(time.Duration(delay), cfg.Jitter.Duration)
}

// Suppressed returns true when the next calculated delay would equal max_delay,
// meaning the issue is recurring and the operator should intervene.
func Suppressed(reboots []time.Time, cfg config.BackoffConfig) bool {
	now := time.Now()
	count := 0
	for _, t := range reboots {
		if now.Sub(t) <= cfg.Window.Duration {
			count++
		}
	}
	if count == 0 {
		return false
	}
	delay := float64(cfg.BaseDelay.Duration) * math.Pow(cfg.Multiplier, float64(count))
	return delay >= float64(cfg.MaxDelay.Duration)
}

func addJitter(base, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(jitter)))
}

// PruneOld removes reboot timestamps older than 2× the backoff window.
func PruneOld(reboots []time.Time, window time.Duration) []time.Time {
	cutoff := time.Now().Add(-2 * window)
	var out []time.Time
	for _, t := range reboots {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}
