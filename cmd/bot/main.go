// Command bot is the Discord bot: friends message it /status, /start and
// /stop, and it calls the control API for them. Configuration comes from
// environment variables; see .env.example in the repo root. The API must
// be running first: the bot asks it which games exist.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Pigoennn/server-control-panel/internal/bot"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Same shape as cmd/api: run returns errors, main turns one into an
	// exit code.
	if err := run(logger); err != nil {
		logger.Error("bot stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	// Safe to log whole: config.LogValue leaves both tokens out.
	logger.Info("bot starting", "config", cfg)

	// ctx is cancelled on Ctrl-C (SIGINT) or `kill`/systemd stop (SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := bot.NewHTTPClient(cfg.APIURL, cfg.APIToken)

	// The game choices for /start and /stop come from the API, once, at
	// startup. A game added to the API later needs a bot restart.
	names, err := gameNames(ctx, api)
	if err != nil {
		return fmt.Errorf("control API not reachable at %s: start it first (go run ./cmd/api): %w", cfg.APIURL, err)
	}

	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return err // discordgo.New only builds the struct; this doesn't include the token
	}
	// Slash commands and buttons arrive without any gateway intents: the
	// bot reads no messages and watches no servers.
	s.Identify.Intents = discordgo.IntentsNone

	b := bot.New(bot.Config{
		API:         api,
		Allowed:     cfg.Allowed,
		Admins:      cfg.Admins,
		JoinAddress: cfg.JoinAddress,
		Logger:      logger,
	})
	bot.Attach(ctx, s, b, logger)

	if err := s.Open(); err != nil {
		return fmt.Errorf("connect to Discord: %w", err)
	}
	defer s.Close()

	// After Open, s.State.User is the bot's own user, whose ID is the
	// application ID that commands are registered under.
	appID := s.State.User.ID
	cmds := bot.Commands(names, cfg.GuildID != "")
	// BulkOverwrite replaces the whole set, so a command removed here
	// disappears from Discord too. guildID "" means global.
	if _, err := s.ApplicationCommandBulkOverwrite(appID, cfg.GuildID, cmds, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	where := "globally, for DMs (can take up to an hour to show)"
	if cfg.GuildID != "" {
		where = "to server " + cfg.GuildID
	}
	logger.Info("bot ready", "user", s.State.User.Username, "games", names, "commands", where)

	<-ctx.Done()
	logger.Info("bot shutting down")
	return nil
}

// gameNames asks the API which games exist, giving up after 5 seconds.
func gameNames(ctx context.Context, api bot.APIClient) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	gs, err := api.Games(ctx, bot.Actor{ID: "bot-startup"})
	if err != nil {
		return nil, err
	}
	if len(gs) == 0 {
		return nil, errors.New("the API has no games")
	}
	names := make([]string, 0, len(gs))
	for _, g := range gs {
		names = append(names, g.Name)
	}
	return names, nil
}
