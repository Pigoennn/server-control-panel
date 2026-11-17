package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"github.com/Pigoennn/server-control-panel/internal/games"
)

// newLoggedAPI builds the API with a JSON logger writing into buf.
func newLoggedAPI(buf *bytes.Buffer) http.Handler {
	return New(Config{
		Games:   []games.Game{oneGame()},
		Token:   testToken,
		Logger:  slog.New(slog.NewJSONHandler(buf, nil)),
		Limiter: rate.NewLimiter(rate.Inf, 0),
	})
}

func TestActorLogged(t *testing.T) {
	var buf bytes.Buffer
	doAs(t, newLoggedAPI(&buf), "GET", "/games", Actor{ID: "123", Admin: true})

	log := buf.String()
	if !strings.Contains(log, `"actor":"123"`) || !strings.Contains(log, `"admin":true`) {
		t.Errorf("log has no actor fields:\n%s", log)
	}
}

func TestNoActorIsNonAdmin(t *testing.T) {
	var buf bytes.Buffer
	do(t, newLoggedAPI(&buf), "GET", "/games")

	log := buf.String()
	if !strings.Contains(log, `"actor":""`) || !strings.Contains(log, `"admin":false`) {
		t.Errorf("log should show an anonymous non-admin:\n%s", log)
	}
}

// Only the exact value "true" makes someone an admin. Being strict costs
// nothing (the bot always sends "true") and avoids surprises.
func TestAdminHeaderOnlyTrue(t *testing.T) {
	for _, v := range []string{"TRUE", "1", "yes"} {
		var got Actor
		h := withActor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = actorFrom(r.Context())
		}))
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Actor-ID", "123")
		req.Header.Set("X-Actor-Admin", v)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got.Admin {
			t.Errorf("X-Actor-Admin: %s made an admin", v)
		}
		if got.ID != "123" {
			t.Errorf("ID = %q, want 123", got.ID)
		}
	}
}
