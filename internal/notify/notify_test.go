package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
	"github.com/pgc-devops/mountsentinel/internal/state"
)

func TestWriteZabbixState_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")

	cfg := config.ZabbixConfig{Enabled: true, StateFile: path}

	st := state.New()
	now := time.Now()
	st.Mounts["/data"] = &state.MountRecord{
		Mountpoint: "/data",
		Device:     "/dev/sdb1",
		Label:      "iscsi-data",
		State:      state.StateDetected,
		Reboots:    []time.Time{now.Add(-1 * time.Hour)},
	}

	WriteZabbixState(cfg, st)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected Zabbix state file to be created: %v", err)
	}

	var zs ZabbixState
	if err := json.Unmarshal(data, &zs); err != nil {
		t.Fatalf("invalid Zabbix state JSON: %v", err)
	}

	if len(zs.Discovery) != 1 {
		t.Errorf("expected 1 discovery entry, got %d", len(zs.Discovery))
	}
	item, ok := zs.Items["/data"]
	if !ok {
		t.Fatal("expected /data in items")
	}
	if item.State != "DETECTED" {
		t.Errorf("expected state DETECTED, got %s", item.State)
	}
	if item.RebootCount != 1 {
		t.Errorf("expected reboot_count 1, got %d", item.RebootCount)
	}
}

func TestWriteZabbixState_Disabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")

	cfg := config.ZabbixConfig{Enabled: false, StateFile: path}
	WriteZabbixState(cfg, state.New())

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not be created when Zabbix disabled")
	}
}

func TestWriteZabbixState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")
	cfg := config.ZabbixConfig{Enabled: true, StateFile: path}

	WriteZabbixState(cfg, state.New())

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file must not remain after write")
	}
}

func TestFormatZabbixDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")
	cfg := config.ZabbixConfig{Enabled: true, StateFile: path}

	st := state.New()
	st.Mounts["/data"] = &state.MountRecord{
		Mountpoint: "/data",
		Device:     "/dev/sdb1",
		Label:      "iscsi",
		State:      state.StateHealthy,
	}
	WriteZabbixState(cfg, st)

	out, err := FormatZabbixDiscovery(path)
	if err != nil {
		t.Fatalf("FormatZabbixDiscovery: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("discovery output not valid JSON: %v", err)
	}
	data, ok := result["data"].([]any)
	if !ok || len(data) == 0 {
		t.Error("expected non-empty data array")
	}
}

func TestFormatZabbixValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")
	cfg := config.ZabbixConfig{Enabled: true, StateFile: path}

	st := state.New()
	st.Mounts["/data"] = &state.MountRecord{
		Mountpoint: "/data",
		Device:     "/dev/sdb1",
		State:      state.StateSuppressed,
		Reboots:    []time.Time{time.Now()},
		Suppressed: true,
	}
	WriteZabbixState(cfg, st)

	cases := []struct {
		key  string
		want string
	}{
		{"state", "SUPPRESSED"},
		{"reboot_count", "1"},
		{"suppressed", "1"},
	}
	for _, tc := range cases {
		val, err := FormatZabbixValue(path, "/data", tc.key)
		if err != nil {
			t.Errorf("FormatZabbixValue(%s): %v", tc.key, err)
			continue
		}
		if val != tc.want {
			t.Errorf("key %s: expected %q, got %q", tc.key, tc.want, val)
		}
	}
}

func TestFormatZabbixValue_UnknownMount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zabbix.json")
	cfg := config.ZabbixConfig{Enabled: true, StateFile: path}
	WriteZabbixState(cfg, state.New())

	_, err := FormatZabbixValue(path, "/nonexistent", "state")
	if err == nil {
		t.Error("expected error for unknown mount")
	}
}

func TestWebhook_SendsRequest(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.WebhookConfig{
		URL:    srv.URL,
		Method: "POST",
	}
	Webhook(cfg, Event{
		Hostname:   "vm01",
		Mountpoint: "/data",
		Event:      "DETECTED",
	})

	if len(received) == 0 {
		t.Error("expected webhook body to be received")
	}
}

func TestWebhook_NilConfig(t *testing.T) {
	// Must not panic.
	Webhook(nil, Event{})
}

func TestWebhook_Template(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.WebhookConfig{
		URL:          srv.URL,
		BodyTemplate: `{"host":"{{.Hostname}}","event":"{{.Event}}"}`,
	}
	Webhook(cfg, Event{Hostname: "vm01", Event: "REBOOTING"})

	if body != `{"host":"vm01","event":"REBOOTING"}` {
		t.Errorf("unexpected body: %s", body)
	}
}
