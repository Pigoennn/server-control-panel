package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// config is everything the bot reads from the environment (the repo-root
// .env, shared with the API).
type config struct {
	DiscordToken string          // DISCORD_TOKEN, required
	DevGuildID   string          // DISCORD_DEV_GUILD_ID, optional: register commands to this test server only
	Allowed      map[string]bool // ALLOWED_IDS plus ADMIN_IDS: who may use the bot
	Admins       map[string]bool // ADMIN_IDS
	APIURL       string          // API_URL, default http://127.0.0.1:8080
	APIToken     string          // API_TOKEN, required (the same token the API checks)
	JoinAddress  string          // JOIN_ADDRESS, optional, e.g. scooter-swizzle.tun.ply.gg
}

// loadConfig reads the config through getenv: os.Getenv in main, a map
// lookup in tests (the same pattern as cmd/api).
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		DiscordToken: getenv("DISCORD_TOKEN"),
		DevGuildID:   strings.TrimSpace(getenv("DISCORD_DEV_GUILD_ID")),
		APIURL:       orDefault(getenv("API_URL"), "http://127.0.0.1:8080"),
		APIToken:     getenv("API_TOKEN"),
		JoinAddress:  strings.TrimSpace(getenv("JOIN_ADDRESS")),
	}

	switch {
	case cfg.DiscordToken == "":
		return config{}, errors.New("DISCORD_TOKEN is required")
	case cfg.APIToken == "":
		return config{}, errors.New("API_TOKEN is required")
	}

	allowed, err := parseIDs("ALLOWED_IDS", getenv("ALLOWED_IDS"))
	if err != nil {
		return config{}, err
	}
	admins, err := parseIDs("ADMIN_IDS", getenv("ADMIN_IDS"))
	if err != nil {
		return config{}, err
	}
	if len(allowed) == 0 && len(admins) == 0 {
		return config{}, errors.New("ALLOWED_IDS and ADMIN_IDS are both empty: nobody could use the bot")
	}

	// Admins are allowed automatically, so a user listed only in
	// ADMIN_IDS can still use the bot. The bot then checks one map,
	// Allowed, for "may use it at all".
	for id := range admins {
		allowed[id] = true
	}
	cfg.Allowed = allowed
	cfg.Admins = admins
	return cfg, nil
}

// parseIDs reads a comma-separated list of Discord user IDs into a set.
//
// Go has no set type; a map[string]bool is the usual stand-in. Looking up
// a missing key gives the zero value, false, so cfg.Allowed[id] reads as
// "is id allowed?" with no comma-ok needed.
//
// Spaces around IDs and empty items are ignored, so "1, 2," is fine. name
// is the variable's name, for the error message.
func parseIDs(name, list string) (map[string]bool, error) {
	set := make(map[string]bool)
	for item := range strings.SplitSeq(list, ",") {
		id := strings.TrimSpace(item)
		if id == "" {
			continue
		}
		if !isUserID(id) {
			return nil, fmt.Errorf("%s: %q is not a Discord user ID", name, id)
		}
		set[id] = true
	}
	return set, nil
}

// isUserID reports whether s looks like a Discord user ID: a "snowflake",
// 17 to 20 decimal digits. Catching a username or a stray character here
// beats silently allowing nobody.
func isUserID(s string) bool {
	if len(s) < 17 || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// LogValue is how slog prints a config. Any type with this method
// implements slog.LogValuer, and slog calls it instead of printing the
// fields itself. That means `logger.Info("starting", "config", cfg)` is
// safe: the tokens are never written, only whether they're set. Listing
// fields here is opt-in, so a secret added to config later stays out of
// the logs until someone deliberately adds it.
func (c config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("api_url", c.APIURL),
		slog.String("dev_guild_id", c.DevGuildID),
		slog.String("join_address", c.JoinAddress),
		slog.Int("allowed", len(c.Allowed)),
		slog.Int("admins", len(c.Admins)),
	)
}

// orDefault returns v, or def if v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
