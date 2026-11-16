package perf

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultConfigIsR14s(t *testing.T) {
	d := DefaultConfig()
	want := Config{Events: 1000000, Notes: 50000, Leases: 200, IdleSeconds: 10, Requests: 2000, LoadSeconds: 60,
		LoadRate: 200, SSEClients: 500, CLIRuns: 50}
	if d != want {
		t.Fatalf("DefaultConfig = %+v, want %+v", d, want)
	}
	if d.Reduced() {
		t.Fatal("the default run is not a reduced one")
	}
	got, err := ConfigFromEnv(envOf(nil))
	if err != nil || got != d {
		t.Fatalf("no settings: %+v, %v", got, err)
	}
}

func TestEverySettingSizesItsField(t *testing.T) {
	for _, s := range settings {
		// 7 events with the default 50,000 notes is refused, so size the notes too.
		env := map[string]string{"VH_PERF_NOTES": "3", s.name: "7"}
		cfg, err := ConfigFromEnv(envOf(env))
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := *s.field(&cfg); got != 7 {
			t.Errorf("%s = %d, want 7", s.name, got)
		}
		if !cfg.Reduced() {
			t.Errorf("%s=7 should make the run reduced", s.name)
		}
	}
	if len(settings) != 9 {
		t.Errorf("%d settings, want one per Config field (9)", len(settings))
	}
}

func TestEachFieldAloneMakesTheRunReduced(t *testing.T) {
	for _, s := range settings {
		cfg := DefaultConfig()
		*s.field(&cfg)--
		if !cfg.Reduced() {
			t.Errorf("%s one below the default is not reduced", s.name)
		}
		cfg = DefaultConfig()
		*s.field(&cfg)++
		if cfg.Reduced() {
			t.Errorf("%s above the default is reduced", s.name)
		}
	}
}

func TestBadSettingsAreRefused(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"not a number":           {"VH_PERF_EVENTS": "many"},
		"zero":                   {"VH_PERF_LEASES": "0"},
		"negative":               {"VH_PERF_SSE_CLIENTS": "-5"},
		"more notes than events": {"VH_PERF_EVENTS": "100", "VH_PERF_NOTES": "101"},
	} {
		if _, err := ConfigFromEnv(envOf(env)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	_, err := ConfigFromEnv(envOf(map[string]string{"VH_PERF_EVENTS": "x"}))
	if err == nil || !strings.Contains(err.Error(), "VH_PERF_EVENTS") {
		t.Errorf("the error should name the variable: %v", err)
	}
}
