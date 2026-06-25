package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
	"github.com/pgc-devops/mountsentinel/internal/logger"
	"github.com/pgc-devops/mountsentinel/internal/state"
)

// Event is the data passed to webhook templates and notification calls.
type Event struct {
	Hostname   string
	Mountpoint string
	Device     string
	Label      string
	Event      string // DETECTED | REBOOTING | RECOVERED | SUPPRESSED
	State      state.MountState
	RebootAt   *time.Time
	RebootCount int
	BackoffDelay time.Duration
	DryRun     bool
}

// Webhook sends a templated HTTP notification.
func Webhook(cfg *config.WebhookConfig, ev Event) {
	if cfg == nil || cfg.URL == "" {
		return
	}

	body, err := renderTemplate(cfg.BodyTemplate, ev)
	if err != nil {
		logger.Error("webhook_template_error", map[string]any{"error": err.Error()})
		return
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}
	timeout := cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(method, cfg.URL, bytes.NewBufferString(body))
	if err != nil {
		logger.Error("webhook_request_error", map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("webhook_send_error", map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		logger.Warn("webhook_non2xx", map[string]any{"status": resp.StatusCode, "url": cfg.URL})
		return
	}
	logger.Debug("webhook_sent", map[string]any{"event": ev.Event, "status": resp.StatusCode})
}

func renderTemplate(tmplStr string, ev Event) (string, error) {
	if tmplStr == "" {
		b, _ := json.Marshal(ev)
		return string(b), nil
	}
	t, err := template.New("webhook").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ev); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ZabbixState is the structure written to the Zabbix state file.
type ZabbixState struct {
	UpdatedAt string                      `json:"updated_at"`
	Discovery []map[string]string         `json:"discovery"`
	Items     map[string]ZabbixMountItem  `json:"items"`
}

type ZabbixMountItem struct {
	State        string `json:"state"`
	RebootCount  int    `json:"reboot_count"`
	LastEvent    string `json:"last_event,omitempty"`
	BackoffDelaySec int64 `json:"backoff_delay_sec"`
	Suppressed   bool   `json:"suppressed"`
}

// WriteZabbixState atomically writes /run/mountsentinel/zabbix.json (or configured path).
// This file lives on tmpfs and is always writable even when data mounts are read-only.
func WriteZabbixState(cfg config.ZabbixConfig, st *state.State) {
	if !cfg.Enabled || cfg.StateFile == "" {
		return
	}

	zs := ZabbixState{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Discovery: []map[string]string{},
		Items:     make(map[string]ZabbixMountItem),
	}

	for _, r := range st.Mounts {
		disc := map[string]string{
			"{#MOUNT}":  r.Mountpoint,
			"{#DEVICE}": r.Device,
			"{#LABEL}":  r.Label,
		}
		zs.Discovery = append(zs.Discovery, disc)

		lastEvent := ""
		if len(r.Reboots) > 0 {
			lastEvent = r.Reboots[len(r.Reboots)-1].UTC().Format(time.RFC3339)
		}

		zs.Items[r.Mountpoint] = ZabbixMountItem{
			State:       string(r.State),
			RebootCount: len(r.Reboots),
			LastEvent:   lastEvent,
			Suppressed:  r.Suppressed,
		}
	}

	data, err := json.MarshalIndent(zs, "", "  ")
	if err != nil {
		logger.Error("zabbix_marshal_error", map[string]any{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(filepath.Dir(cfg.StateFile), 0755); err != nil {
		logger.Error("zabbix_mkdir_error", map[string]any{"error": err.Error()})
		return
	}

	tmp := cfg.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		logger.Error("zabbix_write_error", map[string]any{"error": err.Error()})
		return
	}
	if err := os.Rename(tmp, cfg.StateFile); err != nil {
		logger.Error("zabbix_rename_error", map[string]any{"error": err.Error()})
	}
}

// FormatZabbixDiscovery prints the LLD discovery JSON for use by UserParameter.
func FormatZabbixDiscovery(zabbixFile string) (string, error) {
	data, err := os.ReadFile(zabbixFile)
	if err != nil {
		return "", fmt.Errorf("read zabbix state: %w", err)
	}
	var zs ZabbixState
	if err := json.Unmarshal(data, &zs); err != nil {
		return "", err
	}
	out := map[string]any{"data": zs.Discovery}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FormatZabbixValue returns a single item value from the Zabbix state file.
func FormatZabbixValue(zabbixFile, mountpoint, key string) (string, error) {
	data, err := os.ReadFile(zabbixFile)
	if err != nil {
		return "", fmt.Errorf("read zabbix state: %w", err)
	}
	var zs ZabbixState
	if err := json.Unmarshal(data, &zs); err != nil {
		return "", err
	}
	item, ok := zs.Items[mountpoint]
	if !ok {
		return "", fmt.Errorf("mount %q not found in zabbix state", mountpoint)
	}
	switch key {
	case "state":
		return item.State, nil
	case "reboot_count":
		return fmt.Sprintf("%d", item.RebootCount), nil
	case "last_event":
		return item.LastEvent, nil
	case "backoff_delay_sec":
		return fmt.Sprintf("%d", item.BackoffDelaySec), nil
	case "suppressed":
		if item.Suppressed {
			return "1", nil
		}
		return "0", nil
	default:
		return "", fmt.Errorf("unknown key %q", key)
	}
}
