package main

import (
	"errors"
	"fmt"
)

// minTokenLen is the shortest API_TOKEN accepted. `openssl rand -hex 32`
// gives 64 characters; 32 is the floor so a typo'd short token fails fast.
const minTokenLen = 32

// config is everything the API reads from the environment.
//
// DOCKER_HOST isn't here: the Docker SDK reads it itself (client.FromEnv).
type config struct {
	Token        string // API_TOKEN, required
	Addr         string // API_ADDR, default 127.0.0.1:8080
	RCONAddr     string // MC_RCON_ADDR, default 127.0.0.1:25575
	RCONPassword string // MC_RCON_PASSWORD, required
}

// loadConfig reads the config through getenv, which is os.Getenv in main
// and a map lookup in tests. Passing the function in is dependency
// injection at its smallest: no globals, nothing to reset between tests.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Token:        getenv("API_TOKEN"),
		Addr:         orDefault(getenv("API_ADDR"), "127.0.0.1:8080"),
		RCONAddr:     orDefault(getenv("MC_RCON_ADDR"), "127.0.0.1:25575"),
		RCONPassword: getenv("MC_RCON_PASSWORD"),
	}

	switch {
	case cfg.Token == "":
		return config{}, errors.New("API_TOKEN is required")
	case len(cfg.Token) < minTokenLen:
		return config{}, fmt.Errorf("API_TOKEN must be at least %d characters", minTokenLen)
	case cfg.RCONPassword == "":
		return config{}, errors.New("MC_RCON_PASSWORD is required")
	}
	return cfg, nil
}

// orDefault returns v, or def if v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
