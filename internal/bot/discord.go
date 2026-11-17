package bot

// This file is the only part of the bot that knows about Discord. It turns
// Discord interactions into HandleCommand and HandleButton calls, and
// Replies into Discord message edits. It holds no decisions of its own,
// so it has no unit tests: bot.go's tests cover the logic, and a manual
// run covers this translation.

import (
	"context"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
)

// handlerTimeout bounds one interaction's work. Discord stops accepting
// edits 15 minutes after the interaction, and the bot gives up polling at
// editWindow (14 minutes); this is a backstop a little past that.
const handlerTimeout = editWindow + time.Minute

// Commands returns the slash commands to register. games are the choices
// for the game option of /start and /stop.
//
// In production (devGuild false) they're global commands for a
// user-installed app, usable only in a DM with the bot. In development
// they're registered to the test server instead, where updates show up
// at once rather than up to an hour later.
func Commands(games []string, devGuild bool) []*discordgo.ApplicationCommand {
	integrations := []discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationUserInstall}
	contexts := []discordgo.InteractionContextType{discordgo.InteractionContextBotDM}
	if devGuild {
		integrations = []discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationGuildInstall}
		contexts = []discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(games))
	for _, g := range games {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: g, Value: g})
	}
	gameOption := func(description string) []*discordgo.ApplicationCommandOption {
		return []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "game",
			Description: description,
			Required:    true,
			Choices:     choices,
		}}
	}

	// discordgo uses pointers to slices for these fields, so that "not
	// set" (nil) and "set to empty" can be told apart in the JSON.
	cmd := func(name, description string, options []*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommand {
		return &discordgo.ApplicationCommand{
			Name:             name,
			Description:      description,
			Options:          options,
			IntegrationTypes: &integrations,
			Contexts:         &contexts,
		}
	}
	return []*discordgo.ApplicationCommand{
		cmd("status", "Which game servers are up, and who's on", nil),
		cmd("start", "Start a game server", gameOption("The game to start")),
		cmd("stop", "Stop a game server", gameOption("The game to stop")),
	}
}

// Attach routes s's interactions to b. Each runs on its own goroutine
// (discordgo's default), so a /start that polls for minutes doesn't hold
// up anyone else. ctx is the bot's lifetime: cancelling it (on shutdown)
// stops any polling still going.
func Attach(ctx context.Context, s *discordgo.Session, b *Bot, logger *slog.Logger) {
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		user := userID(i.Interaction)
		if user == "" {
			logger.Error("interaction without a user", "type", i.Type)
			return
		}

		// Acknowledge first, within Discord's 3 seconds, before any API
		// call: a stop or an RCON timeout can take far longer. The user
		// sees "Bot is thinking..." until the first Edit.
		var ack discordgo.InteractionResponseType
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			ack = discordgo.InteractionResponseDeferredChannelMessageWithSource
		case discordgo.InteractionMessageComponent:
			ack = discordgo.InteractionResponseDeferredMessageUpdate
		default:
			return // autocomplete, modals: not used
		}
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: ack,
			// Ephemeral: only the person who asked sees the reply.
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		})
		if err != nil {
			logger.Error("acknowledge interaction", "user", user, "error", err)
			return
		}

		ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
		defer cancel()
		r := &responder{s: s, i: i.Interaction}

		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			data := i.ApplicationCommandData()
			game := ""
			for _, opt := range data.Options {
				if opt.Name == "game" {
					game = opt.StringValue()
				}
			}
			logger.Info("command", "user", user, "command", data.Name, "game", game)
			b.HandleCommand(ctx, user, data.Name, game, r)
		case discordgo.InteractionMessageComponent:
			id := i.MessageComponentData().CustomID
			logger.Info("button", "user", user, "button", id)
			b.HandleButton(ctx, user, id, r)
		}
	})
}

// userID is who triggered i: i.User in a DM, i.Member.User in a server.
func userID(i *discordgo.Interaction) string {
	switch {
	case i.User != nil:
		return i.User.ID
	case i.Member != nil && i.Member.User != nil:
		return i.Member.User.ID
	}
	return ""
}

// responder is the Discord Responder: every Reply edits the interaction's
// deferred response.
type responder struct {
	s *discordgo.Session
	i *discordgo.Interaction
}

var _ Responder = (*responder)(nil)

func (r *responder) Edit(ctx context.Context, reply Reply) error {
	// An empty (but non-nil) slice is sent as "components": [], which
	// removes the old buttons. A nil pointer would leave them untouched.
	components := []discordgo.MessageComponent{}
	if len(reply.Buttons) > 0 {
		row := discordgo.ActionsRow{}
		for _, b := range reply.Buttons {
			style := discordgo.SecondaryButton
			if b.Danger {
				style = discordgo.DangerButton
			}
			row.Components = append(row.Components, discordgo.Button{
				Label:    b.Label,
				Style:    style,
				CustomID: b.ID,
			})
		}
		components = append(components, row)
	}

	_, err := r.s.InteractionResponseEdit(r.i, &discordgo.WebhookEdit{
		Content:    &reply.Content,
		Components: &components,
	}, discordgo.WithContext(ctx))
	return err
}
