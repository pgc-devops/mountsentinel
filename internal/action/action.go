package action

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
	"github.com/pgc-devops/mountsentinel/internal/logger"
)

// RunHooks executes pre-reboot hooks in order. A hook failure is logged but
// does not prevent subsequent hooks or the reboot from proceeding.
func RunHooks(hooks []config.HookConfig) {
	for _, h := range hooks {
		timeout := h.Timeout.Duration
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, h.Cmd, h.Args...)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			logger.Warn("pre_reboot_hook_failed", map[string]any{
				"cmd":    h.Cmd,
				"error":  err.Error(),
				"output": string(out),
			})
		} else {
			logger.Debug("pre_reboot_hook_ok", map[string]any{"cmd": h.Cmd})
		}
	}
}

// Reboot triggers a system reboot. In dry_run mode it only logs the intent.
// Requires root (uid 0) to execute.
func Reboot(dryRun bool) error {
	if dryRun {
		logger.Info("dry_run_reboot_skipped", map[string]any{
			"msg": "would execute: systemctl reboot",
		})
		return nil
	}

	if os.Getuid() != 0 {
		return fmt.Errorf("reboot requires root privileges (uid=%d)", os.Getuid())
	}

	logger.Info("executing_reboot")

	// Try systemctl first; fall back to reboot(8) for minimal environments.
	if err := exec.Command("systemctl", "reboot").Run(); err != nil {
		logger.Warn("systemctl_reboot_failed_trying_reboot_cmd", map[string]any{"error": err.Error()})
		if err2 := exec.Command("reboot", "-f").Run(); err2 != nil {
			return fmt.Errorf("reboot failed: systemctl: %v, reboot -f: %v", err, err2)
		}
	}
	return nil
}
