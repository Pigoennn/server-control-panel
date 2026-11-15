package main

import (
	"strings"
	"testing"
)

const goodToken = "0123456789abcdef0123456789abcdef" // 32 characters

// env turns a map into a getenv function. loadConfig takes getenv as a
// parameter instead of calling os.Getenv itself, so tests can hand it a
// fake environment without touching the real one.
func env(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"API_TOKEN":        goodToken,
		"MC_RCON_PASSWORD": "pw",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want 127.0.0.1:8080", cfg.Addr)
	}
	if cfg.RCONAddr != "127.0.0.1:25575" {
		t.Errorf("RCONAddr = %q, want 127.0.0.1:25575", cfg.RCONAddr)
	}
	if cfg.Token != goodToken || cfg.RCONPassword != "pw" {
		t.Errorf("Token/RCONPassword not copied: %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"API_TOKEN":        goodToken,
		"MC_RCON_PASSWORD": "pw",
		"API_ADDR":         "127.0.0.1:9000",
		"MC_RCON_ADDR":     "127.0.0.1:25576",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.RCONAddr != "127.0.0.1:25576" {
		t.Errorf("overrides ignored: %+v", cfg)
	}
}

// wantErr fails unless loadConfig returned an error with exactly msg.
func wantErr(t *testing.T, vars map[string]string, msg string) {
	t.Helper()
	_, err := loadConfig(env(vars))
	if err == nil {
		t.Fatalf("loadConfig succeeded, want error %q", msg)
	}
	if err.Error() != msg {
		t.Errorf("error = %q, want %q", err, msg)
	}
}

func TestLoadConfigMissingToken(t *testing.T) {
	wantErr(t, map[string]string{"MC_RCON_PASSWORD": "pw"}, "API_TOKEN is required")
}

func TestLoadConfigShortToken(t *testing.T) {
	wantErr(t, map[string]string{
		"API_TOKEN":        strings.Repeat("a", 31),
		"MC_RCON_PASSWORD": "pw",
	}, "API_TOKEN must be at least 32 characters")
}

func TestLoadConfigMissingRCONPassword(t *testing.T) {
	wantErr(t, map[string]string{"API_TOKEN": goodToken}, "MC_RCON_PASSWORD is required")
}
