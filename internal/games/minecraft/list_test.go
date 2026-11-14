package minecraft

import (
	"reflect"
	"testing"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

func TestParseList(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		want    []games.Player
		wantErr bool
	}{
		{"empty, trailing space", "There are 0 of a max of 20 players online: ", []games.Player{}, false},
		{"empty, no trailing space", "There are 0 of a max of 20 players online:", []games.Player{}, false},
		{"one player", "There are 1 of a max of 20 players online: Pigoenn", []games.Player{{Name: "Pigoenn"}}, false},
		{"three players", "There are 3 of a max of 20 players online: Pigoenn, probitbit, Steve",
			[]games.Player{{Name: "Pigoenn"}, {Name: "probitbit"}, {Name: "Steve"}}, false},
		{"trailing newline", "There are 1 of a max of 20 players online: Pigoenn\n", []games.Player{{Name: "Pigoenn"}}, false},
		{"not a list reply", "Unknown command", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseList(tt.reply)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseList() = %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseList() error: %v", err)
			}
			// An empty server must give an empty list, not nil: the API
			// encodes nil as null but an empty slice as [].
			if got == nil {
				t.Fatalf("parseList() = nil, want a non-nil slice")
			}
			// reflect.DeepEqual compares slices element by element.
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseList() = %v, want %v", got, tt.want)
			}
		})
	}
}
