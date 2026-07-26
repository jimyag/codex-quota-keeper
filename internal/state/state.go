package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/renameio/v2"
)

type State struct {
	LastPrimaryResetAt int64                `json:"last_primary_reset_at,omitempty"`
	WindowStarted      map[string]time.Time `json:"window_started,omitempty"`
	PendingCredits     map[string]Pending   `json:"pending_credits,omitempty"`
	RedeemedCredits    map[string]time.Time `json:"redeemed_credits,omitempty"`
	SkippedCredits     map[string]time.Time `json:"skipped_credits,omitempty"`
}

type Pending struct {
	FirstSeen time.Time `json:"first_seen"`
	ExpiresAt time.Time `json:"expires_at"`
}

func Load(path string) (State, error) {
	result := New()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	result.ensureMaps()
	return result, nil
}

func New() State {
	result := State{}
	result.ensureMaps()
	return result
}

func (s *State) Save(path string) error {
	s.ensureMaps()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := renameio.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("atomically replace state: %w", err)
	}
	return nil
}

func (s *State) ensureMaps() {
	if s.WindowStarted == nil {
		s.WindowStarted = make(map[string]time.Time)
	}
	if s.PendingCredits == nil {
		s.PendingCredits = make(map[string]Pending)
	}
	if s.RedeemedCredits == nil {
		s.RedeemedCredits = make(map[string]time.Time)
	}
	if s.SkippedCredits == nil {
		s.SkippedCredits = make(map[string]time.Time)
	}
}
