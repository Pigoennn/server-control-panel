package main

import (
	"bytes"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
)

const (
	discordToken = "discord-test-token"
	apiToken     = "0123456789abcdef0123456789abcdef"
	jonathan     = "111111111111111111"   // 18 digits
	friendID     = "22222222222222222"    // 17 digits, the shortest
	otherFriend  = "33333333333333333333" // 20 digits, the longest
)

// env turns a map into a getenv function, as in cmd/api's tests.
func env(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

// valid is a complete environment. Tests copy it and change one thing.
func valid() map[string]string {
	return map[string]string{
		"DISCORD_TOKEN": discordToken,
		"API_TOKEN":     apiToken,
		"ALLOWED_IDS":   friendID,
		"ADMIN_IDS":     jonathan,
	}
}

// ids returns a set's members, sorted, for easy comparison.
func ids(set map[string]bool) []string {
	return slices.Sorted(maps.Keys(set))
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(valid()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.APIURL != "http://127.0.0.1:8080" {
		t.Errorf("APIURL = %q, want http://127.0.0.1:8080", cfg.APIURL)
	}
	if cfg.DiscordToken != discordToken || cfg.APIToken != apiToken {
		t.Errorf("tokens not copied")
	}
	if cfg.GuildID != "" || cfg.JoinAddress != "" {
		t.Errorf("optional fields set: GuildID=%q JoinAddress=%q", cfg.GuildID, cfg.JoinAddress)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	vars := valid()
	vars["API_URL"] = "http://127.0.0.1:9000"
	vars["DISCORD_GUILD_ID"] = "444444444444444444"
	vars["JOIN_ADDRESS"] = "scooter-swizzle.tun.ply.gg"
	cfg, err := loadConfig(env(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.APIURL != "http://127.0.0.1:9000" || cfg.GuildID != "444444444444444444" || cfg.JoinAddress != "scooter-swizzle.tun.ply.gg" {
		t.Errorf("overrides ignored: APIURL=%q GuildID=%q JoinAddress=%q", cfg.APIURL, cfg.GuildID, cfg.JoinAddress)
	}
}

func TestAdminsAreAllowed(t *testing.T) {
	cfg, err := loadConfig(env(valid()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got, want := ids(cfg.Allowed), []string{jonathan, friendID}; !slices.Equal(got, want) {
		t.Errorf("Allowed = %v, want %v (admins are allowed too)", got, want)
	}
	if got, want := ids(cfg.Admins), []string{jonathan}; !slices.Equal(got, want) {
		t.Errorf("Admins = %v, want %v", got, want)
	}
}

func TestOnlyAdmins(t *testing.T) {
	vars := valid()
	delete(vars, "ALLOWED_IDS")
	cfg, err := loadConfig(env(vars))
	if err != nil {
		t.Fatalf("loadConfig with only ADMIN_IDS: %v", err)
	}
	if !cfg.Allowed[jonathan] {
		t.Errorf("admin %s not allowed", jonathan)
	}
}

func TestIDListSpaces(t *testing.T) {
	vars := valid()
	vars["ALLOWED_IDS"] = " " + friendID + " , " + otherFriend + ","
	cfg, err := loadConfig(env(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got, want := ids(cfg.Allowed), []string{jonathan, friendID, otherFriend}; !slices.Equal(got, want) {
		t.Errorf("Allowed = %v, want %v", got, want)
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

func TestMissingDiscordToken(t *testing.T) {
	vars := valid()
	delete(vars, "DISCORD_TOKEN")
	wantErr(t, vars, "DISCORD_TOKEN is required")
}

func TestMissingAPIToken(t *testing.T) {
	vars := valid()
	delete(vars, "API_TOKEN")
	wantErr(t, vars, "API_TOKEN is required")
}

func TestBothListsEmpty(t *testing.T) {
	vars := valid()
	vars["ALLOWED_IDS"] = " , "
	delete(vars, "ADMIN_IDS")
	wantErr(t, vars, "ALLOWED_IDS and ADMIN_IDS are both empty: nobody could use the bot")
}

func TestBadID(t *testing.T) {
	tests := []struct {
		name, key, value, want string
	}{
		{"letters", "ALLOWED_IDS", "abc", `ALLOWED_IDS: "abc" is not a Discord user ID`},
		{"too short", "ALLOWED_IDS", "123", `ALLOWED_IDS: "123" is not a Discord user ID`},
		{"too long", "ALLOWED_IDS", "123456789012345678901", `ALLOWED_IDS: "123456789012345678901" is not a Discord user ID`},
		{"mixed", "ALLOWED_IDS", "12345678901234567x", `ALLOWED_IDS: "12345678901234567x" is not a Discord user ID`},
		{"admin list", "ADMIN_IDS", jonathan + ",@jonathan", `ADMIN_IDS: "@jonathan" is not a Discord user ID`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := valid()
			vars[tt.key] = tt.value
			wantErr(t, vars, tt.want)
		})
	}
}

// TestConfigLogValueHidesTokens checks that logging the whole config, as
// main does at startup, never writes either token.
func TestConfigLogValueHidesTokens(t *testing.T) {
	cfg, err := loadConfig(env(valid()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("starting", "config", cfg)

	out := buf.String()
	for _, secret := range []string{discordToken, apiToken} {
		if strings.Contains(out, secret) {
			t.Errorf("log line contains a token: %s", out)
		}
	}
	if !strings.Contains(out, `"allowed":2`) || !strings.Contains(out, `"admins":1`) {
		t.Errorf("log line missing the list sizes: %s", out)
	}
}

func TestOldGuildVariable(t *testing.T) {
	vars := valid()
	vars["DISCORD_DEV_GUILD_ID"] = "444444444444444444"
	wantErr(t, vars, "DISCORD_DEV_GUILD_ID was renamed to DISCORD_GUILD_ID: rename it in .env")
}
