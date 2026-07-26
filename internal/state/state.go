package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/natefinch/atomic"
)

type State struct {
	RedeemedCredits map[string]time.Time `json:"redeemed_credits,omitempty"`
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

	if err := atomic.WriteFile(path, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("atomically replace state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set state permissions: %w", err)
	}
	return nil
}

func (s *State) ensureMaps() {
	if s.RedeemedCredits == nil {
		s.RedeemedCredits = make(map[string]time.Time)
	}
}
