package perf

import (
	"fmt"
	"strconv"
)

// Config sizes a run. DefaultConfig is SPEC 14's; every other value is a smoke
// run of the harness, which the report marks as reduced.
type Config struct {
	Events      int `json:"events"`
	Notes       int `json:"notes"`
	Leases      int `json:"leases"`
	IdleSeconds int `json:"idle_seconds"`
	Requests    int `json:"requests"`
	LoadSeconds int `json:"load_seconds"`
	LoadRate    int `json:"load_rate"`
	SSEClients  int `json:"sse_clients"`
	CLIRuns     int `json:"cli_runs"`
}

// DefaultConfig is the run R14 specifies.
func DefaultConfig() Config {
	return Config{Events: 1_000_000, Notes: 50_000, Leases: 200, IdleSeconds: 10, Requests: 2000,
		LoadSeconds: 60, LoadRate: 200, SSEClients: 500, CLIRuns: 50}
}

// Reduced reports whether any size is below what R14 specifies.
func (c Config) Reduced() bool {
	d := DefaultConfig()
	for _, s := range settings {
		if *s.field(&c) < *s.field(&d) {
			return true
		}
	}
	return false
}

// setting is one VH_PERF_ variable and the field it sizes.
type setting struct {
	name  string
	field func(*Config) *int
}

var settings = []setting{
	{"VH_PERF_EVENTS", func(c *Config) *int { return &c.Events }},
	{"VH_PERF_NOTES", func(c *Config) *int { return &c.Notes }},
	{"VH_PERF_LEASES", func(c *Config) *int { return &c.Leases }},
	{"VH_PERF_IDLE_SECONDS", func(c *Config) *int { return &c.IdleSeconds }},
	{"VH_PERF_REQUESTS", func(c *Config) *int { return &c.Requests }},
	{"VH_PERF_LOAD_SECONDS", func(c *Config) *int { return &c.LoadSeconds }},
	{"VH_PERF_LOAD_RATE", func(c *Config) *int { return &c.LoadRate }},
	{"VH_PERF_SSE_CLIENTS", func(c *Config) *int { return &c.SSEClients }},
	{"VH_PERF_CLI_RUNS", func(c *Config) *int { return &c.CLIRuns }},
}

// ConfigFromEnv reads the default run with each VH_PERF_ variable getenv knows
// replacing its field. A value that is not a positive integer is an error, and so
// is a seed with more notes than events (every note has an event).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	for _, s := range settings {
		v := getenv(s.name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("%s=%q: want a positive integer", s.name, v)
		}
		*s.field(&cfg) = n
	}
	if cfg.Notes > cfg.Events {
		return Config{}, fmt.Errorf("VH_PERF_NOTES (%d) is more than VH_PERF_EVENTS (%d)", cfg.Notes, cfg.Events)
	}
	return cfg, nil
}
