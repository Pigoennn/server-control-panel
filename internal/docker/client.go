// Package docker is the thin layer between the game adapters and the Docker
// Engine API. The real work is done by the official SDK
// (github.com/moby/moby/client); this package only narrows it down.
package docker

import (
	"context"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// Client is the part of the Docker SDK the adapters use: inspect, start and
// stop a container. The SDK's *client.Client has dozens of methods; asking
// for only these three means
//
//   - tests can pass a small fake instead of a real Docker daemon, and
//   - the code itself documents that the adapters can't do anything else,
//     matching what the socket proxy allows.
//
// Go idiom: "accept interfaces, return structs". The adapter accepts this
// interface; New returns the concrete SDK type, which satisfies it.
type Client interface {
	ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(ctx context.Context, id string, opts client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(ctx context.Context, id string, opts client.ContainerStopOptions) (client.ContainerStopResult, error)
}

// Compile-time check that the real SDK client satisfies Client.
var _ Client = (*client.Client)(nil)

// New connects to Docker using the standard environment variables, chiefly
// DOCKER_HOST. Point it at the socket proxy (tcp://127.0.0.1:2375), never at
// the raw socket. The API version is negotiated on the first request.
func New() (*client.Client, error) {
	return client.New(client.FromEnv)
}

// IsNotFound reports whether err means the container doesn't exist.
func IsNotFound(err error) bool {
	return cerrdefs.IsNotFound(err)
}
