package minecraft

import (
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// A table-driven test: each row is one case, and the loop runs them all as
// subtests (t.Run), so a failure names the case that broke.
func TestStateFrom(t *testing.T) {
	tests := []struct {
		name string
		in   *container.State
		want games.State
	}{
		{"no container", nil, games.Stopped},
		{"exited", &container.State{Running: false}, games.Stopped},
		{"running, no health check", &container.State{Running: true}, games.Running},
		{"running, health check disabled", &container.State{Running: true, Health: &container.Health{Status: container.NoHealthcheck}}, games.Running},
		{"booting", &container.State{Running: true, Health: &container.Health{Status: container.Starting}}, games.Starting},
		{"healthy", &container.State{Running: true, Health: &container.Health{Status: container.Healthy}}, games.Running},
		{"unhealthy", &container.State{Running: true, Health: &container.Health{Status: container.Unhealthy}}, games.Unhealthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stateFrom(tt.in); got != tt.want {
				t.Errorf("stateFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}
