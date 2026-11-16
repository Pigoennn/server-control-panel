package api

import (
	"context"
	"net/http"
)

// Actor is the Discord user a request is made for. The bot sends it in two
// headers on every call:
//
//	X-Actor-ID:    the Discord user ID, e.g. 123456789012345678
//	X-Actor-Admin: "true" for admins; absent otherwise
//
// The API believes these headers because only a holder of the API token can
// reach /games at all, and today that's the bot (see decision record 0009).
// A request without them (curl from the terminal) is an anonymous non-admin.
type Actor struct {
	ID    string
	Admin bool
}

// actorKey is the key the Actor is stored under in a request's context.
//
// A context holds values under keys of any type, and two keys only match if
// both their type and value match. Using our own unexported type means no
// other package can ever read, overwrite or collide with this value by
// accident, as it could with a plain string key like "actor".
// struct{} is the usual choice: it takes no memory.
type actorKey struct{}

// withActor reads the actor headers into the request's context, where
// handlers (and logRequests) get them with actorFrom.
//
// r.WithContext returns a shallow copy of the request with the new context;
// requests are treated as immutable once created, so middleware passes the
// copy on instead of changing r.
func withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := Actor{
			ID:    r.Header.Get("X-Actor-ID"),
			Admin: r.Header.Get("X-Actor-Admin") == "true",
		}
		ctx := context.WithValue(r.Context(), actorKey{}, a)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// actorFrom returns the request's Actor, or the zero Actor (anonymous,
// not an admin) if withActor didn't run.
//
// The ", ok" form of a type assertion doesn't panic when the value is
// missing or of another type; ok is just false and a is the zero value.
func actorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}
