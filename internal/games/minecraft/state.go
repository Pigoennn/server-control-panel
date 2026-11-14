package minecraft

import (
	"github.com/moby/moby/api/types/container"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// stateFrom turns Docker's view of a container into one of our four states.
// It's the Go version of the `case` in deploy/mc.sh's state().
//
// s is a pointer (*container.State) because Docker may not give one: nil
// means "no container", which counts as stopped.
//
// Lower-case name = unexported: only code in package minecraft can call it.
// Tests in the same package can too, which is why state_test.go says
// `package minecraft`, not `package minecraft_test`.
func stateFrom(s *container.State) games.State {
	//   - nil, or not running            -> games.Stopped
	//   - running with no health info    -> games.Running
	//     (s.Health is nil, or s.Health.Status is container.NoHealthcheck)
	//   - running, health "starting"     -> games.Starting
	//   - running, health "healthy"      -> games.Running
	//   - running, health "unhealthy"    -> games.Unhealthy

	if s == nil || !s.Running {
		return games.Stopped
	}
	if s.Health == nil || s.Health.Status == container.NoHealthcheck {
		return games.Running
	}

	switch s.Health.Status {
	case container.Starting:
		return games.Starting
	case container.Healthy:
		return games.Running
	case container.Unhealthy:
		return games.Unhealthy
	}

	return games.Stopped
}
