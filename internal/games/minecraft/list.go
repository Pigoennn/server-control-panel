package minecraft

import (
	"fmt"
	"strings"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

const expectedPrefix = "There are "

// parseList reads the server's reply to the RCON command "list", e.g.
//
//	There are 2 of a max of 20 players online: Pigoenn, probitbit
//
// and returns the players named after the colon.
func parseList(reply string) ([]games.Player, error) {

	// replies must start with "There are "
	if !strings.HasPrefix(reply, expectedPrefix) {
		return nil, fmt.Errorf("unexpected list reply: %q", reply)
	}

	_, namesString, found := strings.Cut(reply, ":")
	if !found {
		return nil, fmt.Errorf("unexpected list reply: %q", reply)
	}

	// there may be spaces and newlines
	namesString = strings.TrimSpace(namesString)
	if len(namesString) == 0 {
		return make([]games.Player, 0), nil
	}

	// append them
	names := strings.Split(namesString, ", ")
	players := make([]games.Player, 0, len(names))

	for _, name := range names {
		players = append(players, games.Player{Name: name})
	}

	return players, nil
}
