package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/pgc-devops/mountsentinel/internal/logger"
	"github.com/pgc-devops/mountsentinel/internal/state"
)

var (
	mu      sync.RWMutex
	current *state.State
)

// Update replaces the current state snapshot used by the metrics handler.
func Update(st *state.State) {
	mu.Lock()
	current = st
	mu.Unlock()
}

// Serve starts the Prometheus text-format metrics HTTP server.
func Serve(addr string) {
	http.HandleFunc("/metrics", handler)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	logger.Info("metrics_server_starting", map[string]any{"addr": addr})
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Error("metrics_server_error", map[string]any{"error": err.Error()})
	}
}

func stateValue(s state.MountState) int {
	switch s {
	case state.StateHealthy:
		return 0
	case state.StateDetected:
		return 1
	case state.StateSuppressed:
		return 2
	case state.StateRebooting:
		return 3
	default:
		return -1
	}
}

func handler(w http.ResponseWriter, _ *http.Request) {
	mu.RLock()
	st := current
	mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("# HELP mountsentinel_mount_state Current mount state (0=HEALTHY 1=DETECTED 2=SUPPRESSED 3=REBOOTING)\n")
	sb.WriteString("# TYPE mountsentinel_mount_state gauge\n")

	sb.WriteString("# HELP mountsentinel_mount_reboot_total Lifetime reboot count for mount\n")
	sb.WriteString("# TYPE mountsentinel_mount_reboot_total counter\n")

	sb.WriteString("# HELP mountsentinel_mount_suppressed 1 if mount is suppressed (max backoff reached)\n")
	sb.WriteString("# TYPE mountsentinel_mount_suppressed gauge\n")

	if st != nil {
		for _, r := range st.Mounts {
			lbl := fmt.Sprintf(`mount=%q,device=%q,label=%q`, r.Mountpoint, r.Device, r.Label)
			fmt.Fprintf(&sb, "mountsentinel_mount_state{%s} %d\n", lbl, stateValue(r.State))
			fmt.Fprintf(&sb, "mountsentinel_mount_reboot_total{%s} %d\n", lbl, len(r.Reboots))
			sup := 0
			if r.Suppressed {
				sup = 1
			}
			fmt.Fprintf(&sb, "mountsentinel_mount_suppressed{%s} %d\n", lbl, sup)
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprint(w, sb.String())
}
