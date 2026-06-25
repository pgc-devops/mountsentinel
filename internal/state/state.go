package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pgc-devops/mountsentinel/internal/config"
)

type MountState string

const (
	StateHealthy    MountState = "HEALTHY"
	StateDetected   MountState = "DETECTED"
	StateSuppressed MountState = "SUPPRESSED"
	StateRebooting  MountState = "REBOOTING"
)

type MountRecord struct {
	Mountpoint string     `json:"mountpoint"`
	Device     string     `json:"device"`
	Label      string     `json:"label"`
	State      MountState `json:"state"`
	// DetectedAt is when ro was first observed in the current incident.
	DetectedAt *time.Time `json:"detected_at,omitempty"`
	// RebootAt is the scheduled reboot time (DetectedAt + backoff delay).
	RebootAt *time.Time `json:"reboot_at,omitempty"`
	// Reboots is the history of reboot timestamps used for backoff calculation.
	Reboots    []time.Time `json:"reboots,omitempty"`
	Suppressed bool        `json:"suppressed"`
}

type State struct {
	Hostname  string                 `json:"hostname"`
	UpdatedAt time.Time              `json:"updated_at"`
	Mounts    map[string]*MountRecord `json:"mounts"` // keyed by device
}

func New() *State {
	hostname, _ := os.Hostname()
	return &State{
		Hostname:  hostname,
		UpdatedAt: time.Now(),
		Mounts:    make(map[string]*MountRecord),
	}
}

// Store is the interface all backends implement.
type Store interface {
	Load() (*State, error)
	Save(*State) error
}

// NewStore constructs the configured backend with fallback chain.
func NewStore(cfg config.StateConfig) Store {
	primary := newBackend(cfg.Backend, cfg)
	if len(cfg.FallbackBackends) == 0 {
		return primary
	}
	fallbacks := make([]Store, 0, len(cfg.FallbackBackends))
	for _, name := range cfg.FallbackBackends {
		fallbacks = append(fallbacks, newBackend(name, cfg))
	}
	return &fallbackStore{primary: primary, fallbacks: fallbacks}
}

func newBackend(name string, cfg config.StateConfig) Store {
	switch name {
	case "tmpfs":
		return &fileStore{path: "/run/mountsentinel/state.json"}
	case "xenstore":
		return &xenstoreStore{key: "vm-data/mountsentinel/state"}
	case "memory":
		return &memoryStore{}
	case "remote":
		return &remoteStore{url: cfg.RemoteURL}
	default: // "file"
		return &fileStore{path: cfg.FilePath}
	}
}

// --- File backend ---

type fileStore struct{ path string }

func (s *fileStore) Load() (*State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return New(), fmt.Errorf("corrupt state file (reset to clean): %w", err)
	}
	if st.Mounts == nil {
		st.Mounts = make(map[string]*MountRecord)
	}
	return &st, nil
}

func (s *fileStore) Save(st *State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// --- Memory backend ---

type memoryStore struct {
	mu  sync.Mutex
	st  *State
}

func (s *memoryStore) Load() (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st == nil {
		s.st = New()
	}
	return s.st, nil
}

func (s *memoryStore) Save(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = st
	return nil
}

// --- Xenstore backend ---

type xenstoreStore struct{ key string }

func (s *xenstoreStore) Load() (*State, error) {
	out, err := exec.Command("xenstore-read", s.key).Output()
	if err != nil {
		// Key not found: fresh state.
		return New(), nil
	}
	var st State
	if err := json.Unmarshal(out, &st); err != nil {
		return New(), fmt.Errorf("corrupt xenstore state (reset): %w", err)
	}
	if st.Mounts == nil {
		st.Mounts = make(map[string]*MountRecord)
	}
	return &st, nil
}

func (s *xenstoreStore) Save(st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return exec.Command("xenstore-write", s.key, string(data)).Run()
}

// --- Remote HTTP backend ---

type remoteStore struct{ url string }

func (s *remoteStore) Load() (*State, error) {
	resp, err := http.Get(s.url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return New(), nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return New(), err
	}
	if st.Mounts == nil {
		st.Mounts = make(map[string]*MountRecord)
	}
	return &st, nil
}

func (s *remoteStore) Save(st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, s.url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("remote store PUT returned %d", resp.StatusCode)
	}
	return nil
}

// --- Fallback store: tries primary, then fallbacks on save failure ---

type fallbackStore struct {
	primary   Store
	fallbacks []Store
	active    Store
	mu        sync.Mutex
}

func (s *fallbackStore) Load() (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return s.active.Load()
	}
	return s.primary.Load()
}

func (s *fallbackStore) Save(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return s.active.Save(st)
	}
	if err := s.primary.Save(st); err == nil {
		return nil
	}
	for _, fb := range s.fallbacks {
		if err := fb.Save(st); err == nil {
			s.active = fb
			return nil
		}
	}
	return fmt.Errorf("all state backends failed")
}

// ResetDetected resets any DETECTED/SUPPRESSED mounts to HEALTHY on startup.
// Keeps reboot history for backoff accounting. Called after Load() on daemon start.
func ResetDetected(st *State) {
	for _, r := range st.Mounts {
		if r.State == StateDetected || r.State == StateSuppressed {
			r.State = StateHealthy
			r.DetectedAt = nil
			r.RebootAt = nil
		}
	}
}
