package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	CodexBinary  string            `json:"codex_binary"`
	StateFile    string            `json:"state_file"`
	FetchTimeout string            `json:"fetch_timeout"`
	Window       WindowConfig      `json:"window_starter"`
	ResetCredit  ResetCreditConfig `json:"reset_credit"`
}

type WindowConfig struct {
	Enabled      bool     `json:"enabled"`
	WorkingHours string   `json:"working_hours"`
	Command      []string `json:"command"`
	Timeout      string   `json:"timeout"`
}

type ResetCreditConfig struct {
	Enabled            bool   `json:"enabled"`
	RedeemBeforeExpiry string `json:"redeem_before_expiry"`
	AuthFile           string `json:"auth_file"`
	BaseURL            string `json:"base_url"`
	RequestTimeout     string `json:"request_timeout"`
}

type Runtime struct {
	Config
	FetchTimeoutDuration        time.Duration
	WindowTimeoutDuration       time.Duration
	ResetRedeemBeforeDuration   time.Duration
	ResetRequestTimeoutDuration time.Duration
}

func DefaultPath() string {
	return "~/.config/codex-quota-keeper/config.json"
}

func Defaults() Config {
	return Config{
		CodexBinary:  "codex",
		StateFile:    "~/.local/state/codex-quota-keeper/state.json",
		FetchTimeout: "20s",
		Window: WindowConfig{
			Enabled:      false,
			WorkingHours: "09:00-23:00",
			Command: []string{
				"codex",
				"exec",
				"--skip-git-repo-check",
				"--ephemeral",
				"Reply exactly OK.",
			},
			Timeout: "2m",
		},
		ResetCredit: ResetCreditConfig{
			Enabled:            false,
			RedeemBeforeExpiry: "10m",
			AuthFile:           "~/.codex/auth.json",
			BaseURL:            "https://chatgpt.com/backend-api",
			RequestTimeout:     "20s",
		},
	}
}

func Load(path string) (Runtime, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return Runtime{}, err
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		return Runtime{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Runtime{}, fmt.Errorf("decode config: %w", err)
	}

	return cfg.runtime()
}

func WriteDefault(path string) (string, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(expanded); err == nil {
		return "", fmt.Errorf("config already exists: %s", expanded)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(expanded), 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(Defaults(), "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(expanded, data, 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return expanded, nil
}

func (c Config) runtime() (Runtime, error) {
	if strings.TrimSpace(c.CodexBinary) == "" {
		return Runtime{}, errors.New("codex_binary must not be empty")
	}
	if len(c.Window.Command) == 0 {
		return Runtime{}, errors.New("window_starter.command must not be empty")
	}
	if _, _, err := ParseWorkingHours(c.Window.WorkingHours); err != nil {
		return Runtime{}, err
	}

	fetchTimeout, err := positiveDuration("fetch_timeout", c.FetchTimeout)
	if err != nil {
		return Runtime{}, err
	}
	windowTimeout, err := positiveDuration("window_starter.timeout", c.Window.Timeout)
	if err != nil {
		return Runtime{}, err
	}
	redeemBeforeExpiry, err := positiveDuration(
		"reset_credit.redeem_before_expiry",
		c.ResetCredit.RedeemBeforeExpiry,
	)
	if err != nil {
		return Runtime{}, err
	}
	requestTimeout, err := positiveDuration("reset_credit.request_timeout", c.ResetCredit.RequestTimeout)
	if err != nil {
		return Runtime{}, err
	}

	stateFile, err := ExpandPath(c.StateFile)
	if err != nil {
		return Runtime{}, err
	}
	authFile, err := ExpandPath(c.ResetCredit.AuthFile)
	if err != nil {
		return Runtime{}, err
	}
	c.StateFile = stateFile
	c.ResetCredit.AuthFile = authFile
	c.ResetCredit.BaseURL = strings.TrimRight(c.ResetCredit.BaseURL, "/")
	if c.ResetCredit.BaseURL == "" {
		return Runtime{}, errors.New("reset_credit.base_url must not be empty")
	}

	return Runtime{
		Config:                      c,
		FetchTimeoutDuration:        fetchTimeout,
		WindowTimeoutDuration:       windowTimeout,
		ResetRedeemBeforeDuration:   redeemBeforeExpiry,
		ResetRequestTimeoutDuration: requestTimeout,
	}, nil
}

func ExpandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func ParseWorkingHours(value string) (time.Duration, time.Duration, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("window_starter.working_hours must use HH:MM-HH:MM")
	}
	start, err := clockDuration(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid working-hours start: %w", err)
	}
	end, err := clockDuration(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid working-hours end: %w", err)
	}
	return start, end, nil
}

func clockDuration(value string) (time.Duration, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}

func positiveDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return duration, nil
}
