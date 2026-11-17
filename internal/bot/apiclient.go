// Package bot is the Discord bot's logic: who may use it, and what each
// slash command and button does. It talks to the control API through the
// APIClient interface and to Discord through Responder, so tests can use
// fakes for both.
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Actor is the Discord user a request is made for. The API reads it from
// the X-Actor-ID and X-Actor-Admin headers. (It mirrors api.Actor, but the
// bot doesn't import the API's internals: the two only share the HTTP
// contract.)
type Actor struct {
	ID    string
	Admin bool
}

// Game is one game server as the API reports it.
type Game struct {
	Name string `json:"name"`
	// State is "stopped", "starting", "running" or "unhealthy".
	State string `json:"state"`
	// Players is who is online. nil means unknown (the API left the field
	// out); an empty slice means nobody.
	Players []string `json:"players"`
	// StoppingAt is when an admin's countdown stops the server. The zero
	// time.Time means there is no countdown.
	StoppingAt time.Time `json:"stopping_at"`
}

// APIError is a response from the API that wasn't a success (not 2xx).
// Any other error from an APIClient means the API couldn't be reached.
type APIError struct {
	Status  int
	Message string
	// Players is set on a 409 "players are online", when the API knows
	// who is on.
	Players []string
}

// Error makes *APIError satisfy the error interface. The pointer receiver
// means only *APIError is an error, so callers match it with
// errors.As(err, &apiErr) where apiErr is a *APIError.
func (e *APIError) Error() string {
	return fmt.Sprintf("control API: %d %s", e.Status, e.Message)
}

// APIClient is everything the bot asks of the control API.
type APIClient interface {
	Games(ctx context.Context, a Actor) ([]Game, error)
	Game(ctx context.Context, a Actor, name string) (Game, error)
	Start(ctx context.Context, a Actor, name string) (Game, error)
	// Stop reports countdown == true when the API answered 202: an admin
	// stopped a server with players on it, and it stops at g.StoppingAt.
	Stop(ctx context.Context, a Actor, name string) (g Game, countdown bool, err error)
	CancelStop(ctx context.Context, a Actor, name string) (Game, error)
}

// requestTimeout matches the API's WriteTimeout: a blocking stop of the
// modded server can take a couple of minutes.
const requestTimeout = 3 * time.Minute

// maxBody caps how much of a response the client reads. The API's answers
// are tiny; this only guards against something else answering on the port.
const maxBody = 1 << 20 // 1 MiB

// httpClient is the real APIClient. It's unexported: callers get it as an
// APIClient from NewHTTPClient and can't depend on its fields.
type httpClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// Compile-time check that *httpClient implements APIClient.
var _ APIClient = (*httpClient)(nil)

// NewHTTPClient returns an APIClient for the API at baseURL (e.g.
// "http://127.0.0.1:8080"), authenticating with token.
func NewHTTPClient(baseURL, token string) APIClient {
	return &httpClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

func (c *httpClient) Games(ctx context.Context, a Actor) ([]Game, error) {
	gs, _, err := call[[]Game](ctx, c, a, http.MethodGet, "/games")
	return gs, err
}

func (c *httpClient) Game(ctx context.Context, a Actor, name string) (Game, error) {
	g, _, err := call[Game](ctx, c, a, http.MethodGet, gamePath(name))
	return g, err
}

func (c *httpClient) Start(ctx context.Context, a Actor, name string) (Game, error) {
	g, _, err := call[Game](ctx, c, a, http.MethodPost, gamePath(name)+"/start")
	return g, err
}

func (c *httpClient) Stop(ctx context.Context, a Actor, name string) (Game, bool, error) {
	g, status, err := call[Game](ctx, c, a, http.MethodPost, gamePath(name)+"/stop")
	return g, status == http.StatusAccepted, err
}

func (c *httpClient) CancelStop(ctx context.Context, a Actor, name string) (Game, error) {
	g, _, err := call[Game](ctx, c, a, http.MethodPost, gamePath(name)+"/stop/cancel")
	return g, err
}

// gamePath escapes name for use in a URL path, so a name like "../x" stays
// one path segment ("..%2Fx") instead of walking up the path.
func gamePath(name string) string {
	return "/games/" + url.PathEscape(name)
}

// call sends one request and decodes a successful JSON answer into a T.
// It returns the HTTP status so Stop can tell 200 from 202.
//
// It's a generic function, not a method: Go methods can't have their own
// type parameters. [T any] lets one function decode into a Game for most
// calls and into a []Game for Games, with no interface{} and no type
// assertions.
func call[T any](ctx context.Context, c *httpClient, a Actor, method, path string) (T, int, error) {
	var zero T

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return zero, 0, fmt.Errorf("control API %s %s: %w", method, path, err)
	}
	// The token goes only in this header. Errors below mention the method
	// and path, never the request headers, so the token can't leak into a
	// log line through an error message.
	req.Header.Set("Authorization", "Bearer "+c.token)
	if a.ID != "" {
		req.Header.Set("X-Actor-ID", a.ID)
	}
	if a.Admin {
		req.Header.Set("X-Actor-Admin", "true")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Connection refused, timeout, DNS... Wrapped with %w, but not an
		// *APIError, which is how the bot knows the API is down.
		return zero, 0, fmt.Errorf("control API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxBody)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return zero, resp.StatusCode, decodeError(resp.StatusCode, body)
	}

	var v T
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		return zero, resp.StatusCode, fmt.Errorf("control API %s %s: decode response: %w", method, path, err)
	}
	return v, resp.StatusCode, nil
}

// decodeError turns a non-2xx response into an *APIError. The API's errors
// are {"error": "...", "players": [...]}; anything else (a proxy's HTML
// page, an empty body) falls back to the status text, e.g. "Bad Gateway".
func decodeError(status int, body io.Reader) *APIError {
	var e struct {
		Error   string   `json:"error"`
		Players []string `json:"players"`
	}
	if err := json.NewDecoder(body).Decode(&e); err != nil || e.Error == "" {
		return &APIError{Status: status, Message: http.StatusText(status)}
	}
	return &APIError{Status: status, Message: e.Error, Players: e.Players}
}
