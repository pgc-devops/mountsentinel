// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/action"
	"github.com/pgc-devops/mountsentinel/internal/backoff"
	"github.com/pgc-devops/mountsentinel/internal/checker"
	"github.com/pgc-devops/mountsentinel/internal/config"
	"github.com/pgc-devops/mountsentinel/internal/logger"
	"github.com/pgc-devops/mountsentinel/internal/metrics"
	"github.com/pgc-devops/mountsentinel/internal/notify"
	"github.com/pgc-devops/mountsentinel/internal/state"
)

var version = "dev"

func main() {
	var (
		cfgPath string
		verbose bool
		debug   bool
		ver     bool
	)
	flag.StringVar(&cfgPath, "config", "/etc/mountsentinel.yml", "path to config file")
	flag.BoolVar(&verbose, "verbose", false, "verbose logging")
	flag.BoolVar(&debug, "debug", false, "debug logging")
	flag.BoolVar(&ver, "version", false, "print version and exit")
	flag.Parse()

	if ver {
		fmt.Println(version)
		return
	}

	// Subcommands are the first non-flag argument.
	args := flag.Args()
	subcmd := ""
	if len(args) > 0 {
		subcmd = args[0]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mountsentinel: config error: %v\n", err)
		os.Exit(1)
	}

	lvl := logger.ParseLevel(cfg.Daemon.LogLevel)
	if debug {
		lvl = logger.LevelDebug
	}
	if verbose {
		lvl = logger.LevelVerbose
	}
	logger.SetLevel(lvl)

	store := state.NewStore(cfg.State)

	switch subcmd {
	case "status":
		runStatus(args[1:], store, cfg)
		return
	case "reset":
		runReset(args[1:], store)
		return
	}

	runDaemon(cfg, store)
}

// --- status subcommand ---

func runStatus(args []string, store state.Store, cfg *config.Config) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var mountFlag, keyFlag, format string
	fs.StringVar(&mountFlag, "mount", "", "filter by mountpoint")
	fs.StringVar(&keyFlag, "key", "", "specific key to extract")
	fs.StringVar(&format, "format", "table", "output format: table|json|zabbix-discovery|value")
	_ = fs.Parse(args)

	if format == "zabbix-discovery" {
		out, err := notify.FormatZabbixDiscovery(cfg.Zabbix.StateFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}

	if format == "value" {
		val, err := notify.FormatZabbixValue(cfg.Zabbix.StateFile, mountFlag, keyFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(val)
		return
	}

	st, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load state: %v\n", err)
		os.Exit(1)
	}

	// Collect all mountpoints: from state + configured non-wildcards not yet in state.
	type row struct {
		mp      string
		state   state.MountState
		reboots int
		det     string
	}
	rowMap := make(map[string]row)
	for mp, r := range st.Mounts {
		det := "-"
		if r.DetectedAt != nil {
			det = r.DetectedAt.Format(time.RFC3339)
		}
		rowMap[mp] = row{mp: mp, state: r.State, reboots: len(r.Reboots), det: det}
	}
	for _, wm := range cfg.WatchMounts {
		if wm.Mountpoint == "*" {
			continue
		}
		if _, exists := rowMap[wm.Mountpoint]; !exists {
			rowMap[wm.Mountpoint] = row{mp: wm.Mountpoint, state: "UNKNOWN", reboots: 0, det: "-"}
		}
	}

	keys := make([]string, 0, len(rowMap))
	for k := range rowMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	degraded := false
	fmt.Printf("%-30s %-12s %-8s %s\n", "MOUNT", "STATE", "REBOOTS", "DETECTED")
	fmt.Printf("%-30s %-12s %-8s %s\n", "-----", "-----", "-------", "--------")
	for _, mp := range keys {
		if mountFlag != "" && mp != mountFlag {
			continue
		}
		r := rowMap[mp]
		fmt.Printf("%-30s %-12s %-8d %s\n", r.mp, r.state, r.reboots, r.det)
		if r.state != state.StateHealthy && r.state != "UNKNOWN" {
			degraded = true
		}
	}
	if degraded {
		os.Exit(2)
	}
}

// --- reset subcommand ---

func runReset(args []string, store state.Store) {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	var mountFlag string
	fs.StringVar(&mountFlag, "mount", "", "device or mountpoint to reset (required)")
	_ = fs.Parse(args)

	if mountFlag == "" {
		fmt.Fprintln(os.Stderr, "reset: --mount required")
		os.Exit(1)
	}

	st, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load state: %v\n", err)
		os.Exit(1)
	}

	found := false
	for mp, r := range st.Mounts {
		if mp == mountFlag || r.Device == mountFlag {
			r.State = state.StateHealthy
			r.Suppressed = false
			r.DetectedAt = nil
			r.RebootAt = nil
			fmt.Printf("reset %s → HEALTHY\n", r.Mountpoint)
			found = true
		}
	}

	if !found {
		fmt.Fprintf(os.Stderr, "mount %q not found in state\n", mountFlag)
		os.Exit(1)
	}

	if err := store.Save(st); err != nil {
		fmt.Fprintf(os.Stderr, "save state: %v\n", err)
		os.Exit(1)
	}
}

// --- daemon mode ---

func runDaemon(cfg *config.Config, store state.Store) {
	logger.Info("mountsentinel_starting", map[string]any{
		"version":        version,
		"dry_run":        cfg.Daemon.DryRun,
		"reboot_enabled": cfg.Reboot.Enabled,
	})

	if cfg.Reboot.Enabled && !action.HasCapSysBoot() {
		logger.Warn("reboot_enabled_but_unprivileged", map[string]any{
			"uid": os.Getuid(),
			"msg": "reboot will fail at trigger time: process needs root or CAP_SYS_BOOT",
		})
	}

	st, err := store.Load()
	if err != nil {
		logger.Warn("state_load_error_using_clean_state", map[string]any{"error": err.Error()})
		st = state.New()
	}
	// On startup: reset any stale DETECTED states (previous detections before last reboot).
	state.ResetDetected(st)

	if cfg.Metrics.Enabled {
		go metrics.Serve(cfg.Metrics.Addr)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	ticker := time.NewTicker(cfg.Daemon.CheckInterval.Duration)
	defer ticker.Stop()

	// Signal systemd READY=1 via $NOTIFY_SOCKET.
	sdNotify("READY=1")

	// Run an immediate first check.
	st = runCheck(cfg, store, st)

	for {
		select {
		case sig := <-sigs:
			switch sig {
			case syscall.SIGHUP:
				logger.Info("sighup_reloading_config")
				newCfg, err := config.Load(flag.Lookup("config").Value.String())
				if err != nil {
					logger.Error("config_reload_error", map[string]any{"error": err.Error()})
				} else {
					cfg = newCfg
					ticker.Reset(cfg.Daemon.CheckInterval.Duration)
					logger.Info("config_reloaded")
					st = runCheck(cfg, store, st)
				}
			default:
				logger.Info("mountsentinel_stopping", map[string]any{"signal": sig.String()})
				_ = store.Save(st)
				return
			}
		case <-ticker.C:
			sdNotify("WATCHDOG=1")
			st = runCheck(cfg, store, st)
		}
	}
}

func runCheck(cfg *config.Config, store state.Store, st *state.State) *state.State {
	mounts, err := checker.ReadMounts(cfg.Daemon.ProcMountsPath)
	if err != nil {
		logger.Error("proc_mounts_read_error", map[string]any{"error": err.Error()})
		return st
	}

	byMP := checker.IndexByMountpoint(mounts)
	now := time.Now()

	// Build set of watched mountpoints.
	watched := resolveWatched(cfg.WatchMounts, mounts)

	for mp, wm := range watched {
		m, exists := byMP[mp]

		// State is keyed by mountpoint for simplicity and stability.
		rec := st.Mounts[mp]
		if rec == nil {
			dev := wm.Device
			if dev == "" && exists {
				dev = m.Device
			}
			rec = &state.MountRecord{
				Mountpoint: mp,
				Device:     dev,
				Label:      wm.Label,
				State:      state.StateHealthy,
			}
			st.Mounts[mp] = rec
		}

		isRO := exists && m.IsReadOnly()

		switch rec.State {
		case state.StateHealthy:
			if isRO {
				delay := backoff.Calculate(rec.Reboots, cfg.Backoff)
				rebootAt := now.Add(delay)
				rec.State = state.StateDetected
				rec.DetectedAt = &now
				rec.RebootAt = &rebootAt
				rec.Suppressed = false
				logger.Warn("mount_read_only_detected", map[string]any{
					"mount":      mp,
					"device":     rec.Device,
					"reboot_in":  delay.String(),
					"reboot_at":  rebootAt.Format(time.RFC3339),
					"dry_run":    cfg.Daemon.DryRun,
				})
				notify.Webhook(cfg.Notify.Webhook, notify.Event{
					Hostname: st.Hostname, Mountpoint: mp, Device: rec.Device,
					Label: rec.Label, Event: "DETECTED", State: rec.State,
					RebootAt: &rebootAt, RebootCount: len(rec.Reboots),
					BackoffDelay: delay, DryRun: cfg.Daemon.DryRun,
				})
			}

		case state.StateDetected:
			if !isRO {
				// Mount recovered before reboot fired.
				logger.Info("mount_recovered", map[string]any{"mount": mp})
				notify.Webhook(cfg.Notify.Webhook, notify.Event{
					Hostname: st.Hostname, Mountpoint: mp, Device: rec.Device,
					Label: rec.Label, Event: "RECOVERED", State: state.StateHealthy,
					RebootCount: len(rec.Reboots),
				})
				rec.State = state.StateHealthy
				rec.DetectedAt = nil
				rec.RebootAt = nil
			} else if rec.RebootAt != nil && now.After(*rec.RebootAt) {
				// Check if we've hit max backoff.
				if backoff.Suppressed(rec.Reboots, cfg.Backoff) {
					rec.State = state.StateSuppressed
					rec.Suppressed = true
					logger.Warn("mount_suppressed_max_backoff_reached", map[string]any{
						"mount":  mp,
						"msg":    "operator action required: mountsentinel reset --mount " + mp,
					})
					notify.Webhook(cfg.Notify.Webhook, notify.Event{
						Hostname: st.Hostname, Mountpoint: mp, Device: rec.Device,
						Label: rec.Label, Event: "SUPPRESSED", State: rec.State,
						RebootCount: len(rec.Reboots),
					})
				} else {
					triggerReboot(cfg, store, st, rec, mp)
					return st
				}
			}

		case state.StateSuppressed:
			if !isRO {
				logger.Info("suppressed_mount_recovered", map[string]any{"mount": mp})
				rec.State = state.StateHealthy
				rec.Suppressed = false
				rec.DetectedAt = nil
				rec.RebootAt = nil
			} else {
				logger.Debug("mount_still_ro_suppressed", map[string]any{"mount": mp})
			}
		}
	}

	st.UpdatedAt = now
	if err := store.Save(st); err != nil {
		logger.Error("state_save_error", map[string]any{"error": err.Error()})
	}

	if cfg.Metrics.Enabled {
		metrics.Update(st)
	}
	if cfg.Zabbix.Enabled {
		notify.WriteZabbixState(cfg.Zabbix, st)
	}

	logger.Verbose("check_complete", map[string]any{"watched": len(watched)})
	return st
}

func triggerReboot(cfg *config.Config, store state.Store, st *state.State, rec *state.MountRecord, mp string) {
	now := time.Now()
	rec.State = state.StateRebooting
	rec.Reboots = append(rec.Reboots, now)
	rec.Reboots = backoff.PruneOld(rec.Reboots, cfg.Backoff.Window.Duration)
	st.UpdatedAt = now

	logger.Warn("triggering_reboot", map[string]any{
		"mount":        mp,
		"reboot_count": len(rec.Reboots),
		"dry_run":      cfg.Daemon.DryRun,
	})

	notify.Webhook(cfg.Notify.Webhook, notify.Event{
		Hostname: st.Hostname, Mountpoint: mp, Device: rec.Device,
		Label: rec.Label, Event: "REBOOTING", State: rec.State,
		RebootCount: len(rec.Reboots), DryRun: cfg.Daemon.DryRun,
	})

	if cfg.Zabbix.Enabled {
		notify.WriteZabbixState(cfg.Zabbix, st)
	}

	_ = store.Save(st)

	if !cfg.Reboot.Enabled {
		logger.Warn("reboot_disabled_suppressing", map[string]any{
			"mount": mp,
			"msg":   "set reboot.enabled: true and run as root to enable automatic reboot",
		})
		rec.State = state.StateSuppressed
		rec.Suppressed = true
		notify.Webhook(cfg.Notify.Webhook, notify.Event{
			Hostname: st.Hostname, Mountpoint: mp, Device: rec.Device,
			Label: rec.Label, Event: "SUPPRESSED", State: rec.State,
			RebootCount: len(rec.Reboots),
		})
		_ = store.Save(st)
		return
	}

	action.RunHooks(cfg.Reboot.PreRebootHooks)

	if err := action.Reboot(cfg.Daemon.DryRun); err != nil {
		logger.Error("reboot_error", map[string]any{"error": err.Error()})
		// Reset to DETECTED so we retry on next tick.
		rec.State = state.StateDetected
		rebootAt := now.Add(cfg.Backoff.BaseDelay.Duration)
		rec.RebootAt = &rebootAt
	}
}

// resolveWatched expands wildcard watch entries into a map[mountpoint]WatchMount.
func resolveWatched(watches []config.WatchMount, mounts []checker.Mount) map[string]config.WatchMount {
	out := make(map[string]config.WatchMount)
	for _, wm := range watches {
		if wm.Mountpoint == "*" {
			excl := buildExcludeSet(wm.Exclude)
			for _, m := range mounts {
				if excl[m.Mountpoint] {
					continue
				}
				out[m.Mountpoint] = config.WatchMount{
					Mountpoint: m.Mountpoint,
					Device:     m.Device,
					Label:      wm.Label,
				}
			}
		} else {
			out[wm.Mountpoint] = wm
		}
	}
	return out
}

func buildExcludeSet(excludes []string) map[string]bool {
	def := config.DefaultExcludes()
	set := make(map[string]bool, len(def)+len(excludes))
	for _, e := range def {
		set[e] = true
	}
	for _, e := range excludes {
		set[e] = true
	}
	return set
}

// sdNotify writes a notification to systemd's NOTIFY_SOCKET if available.
func sdNotify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	conn, err := connectUnixDgram(sock)
	if err != nil {
		return
	}
	_, _ = conn.Write([]byte(state))
	_ = conn.Close()
}
