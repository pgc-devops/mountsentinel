// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
)

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := &fileStore{path: filepath.Join(dir, "state.json")}

	st := New()
	now := time.Now().UTC().Truncate(time.Second)
	st.Mounts["/data"] = &MountRecord{
		Mountpoint: "/data",
		Device:     "/dev/sdb1",
		State:      StateDetected,
		DetectedAt: &now,
		Reboots:    []time.Time{now.Add(-1 * time.Hour)},
	}

	if err := store.Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rec, ok := loaded.Mounts["/data"]
	if !ok {
		t.Fatal("expected /data in loaded state")
	}
	if rec.State != StateDetected {
		t.Errorf("expected DETECTED, got %s", rec.State)
	}
	if len(rec.Reboots) != 1 {
		t.Errorf("expected 1 reboot, got %d", len(rec.Reboots))
	}
}

func TestFileStore_MissingFile(t *testing.T) {
	store := &fileStore{path: filepath.Join(t.TempDir(), "nonexistent.json")}
	st, err := store.Load()
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	if st == nil || st.Mounts == nil {
		t.Fatal("expected clean state, got nil")
	}
}

func TestFileStore_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0640); err != nil {
		t.Fatal(err)
	}
	store := &fileStore{path: path}
	st, err := store.Load()
	// Corrupt file should return clean state with a non-fatal warning error.
	if st == nil {
		t.Fatal("expected clean state on corrupt file")
	}
	_ = err // corruption returns clean state + wrapped error (both acceptable)
}

func TestFileStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := &fileStore{path: path}

	st := New()
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	// Temp file must not be left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful save")
	}

	// Final file must exist and be valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Errorf("state file is not valid JSON: %v", err)
	}
}

func TestMemoryStore_RoundTrip(t *testing.T) {
	store := &memoryStore{}

	st := New()
	st.Mounts["/tmp/test"] = &MountRecord{State: StateHealthy}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Mounts["/tmp/test"]; !ok {
		t.Error("expected mount in loaded state")
	}
}

func TestMemoryStore_EmptyLoad(t *testing.T) {
	store := &memoryStore{}
	st, err := store.Load()
	if err != nil || st == nil {
		t.Errorf("empty memory store should return clean state: err=%v st=%v", err, st)
	}
}

func TestResetDetected(t *testing.T) {
	st := New()
	now := time.Now()
	st.Mounts["/data"] = &MountRecord{
		State:      StateDetected,
		DetectedAt: &now,
		RebootAt:   &now,
		Reboots:    []time.Time{now},
	}
	st.Mounts["/backup"] = &MountRecord{State: StateSuppressed}
	st.Mounts["/"] = &MountRecord{State: StateHealthy}

	ResetDetected(st)

	if st.Mounts["/data"].State != StateHealthy {
		t.Error("DETECTED should be reset to HEALTHY")
	}
	if st.Mounts["/data"].DetectedAt != nil {
		t.Error("DetectedAt should be cleared")
	}
	if st.Mounts["/data"].RebootAt != nil {
		t.Error("RebootAt should be cleared")
	}
	// Reboot history must be preserved for backoff.
	if len(st.Mounts["/data"].Reboots) != 1 {
		t.Error("reboot history must be preserved after reset")
	}
	if st.Mounts["/backup"].State != StateHealthy {
		t.Error("SUPPRESSED should be reset to HEALTHY")
	}
	if st.Mounts["/"].State != StateHealthy {
		t.Error("HEALTHY should remain HEALTHY")
	}
}

func TestFallbackStore_PrimaryFails(t *testing.T) {
	// Primary: write to a non-writable path (will fail).
	primary := &fileStore{path: "/proc/mountsentinel-test-cannot-write.json"}
	// Fallback: memory.
	mem := &memoryStore{}
	store := &fallbackStore{primary: primary, fallbacks: []Store{mem}}

	st := New()
	if err := store.Save(st); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}

	// Subsequent saves should use the active (memory) backend.
	if store.active != mem {
		t.Error("expected memory backend to become active after primary failure")
	}
}

func TestNewStore_Backends(t *testing.T) {
	cases := []struct {
		backend string
	}{
		{"memory"},
		{"file"},
		{"tmpfs"},
	}
	for _, tc := range cases {
		cfg := config.StateConfig{
			Backend:  tc.backend,
			FilePath: filepath.Join(t.TempDir(), "state.json"),
		}
		store := NewStore(cfg)
		if store == nil {
			t.Errorf("NewStore(%s) returned nil", tc.backend)
		}
	}
}
