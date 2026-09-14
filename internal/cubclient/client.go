// Package cubclient is a thin wrapper around the ConfigHub SDK's goclient-new,
// scoped to what the seeder needs. It reads CUB_SERVER and CUB_TOKEN from the
// environment (set by cub when it invokes a plugin) and adds a Bearer auth
// header to every request.
package cubclient

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Client talks to a ConfigHub server as the current cub session.
type Client struct {
	api    *goclient.ClientWithResponses
	ctx    context.Context
	server string
}

// New constructs a Client from CUB_SERVER and CUB_TOKEN. Both must be set;
// cub populates them when it runs a plugin.
func New(ctx context.Context) (*Client, error) {
	server := os.Getenv("CUB_SERVER")
	if server == "" {
		return nil, fmt.Errorf("CUB_SERVER not set; the demo plugin must be invoked as a cub plugin (try: cub demo ...)")
	}
	token := os.Getenv("CUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("CUB_TOKEN not set; run 'cub auth login' first")
	}

	authHeader := "Bearer " + token
	httpClient := &http.Client{Transport: &retryTransport{next: http.DefaultTransport}, Timeout: 5 * time.Minute}
	api, err := goclient.NewClientWithResponses(strings.TrimRight(server, "/")+"/api",
		goclient.WithHTTPClient(httpClient),
		goclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", authHeader)
			return nil
		}))
	if err != nil {
		return nil, fmt.Errorf("init client: %w", err)
	}
	return &Client{api: api, ctx: ctx, server: server}, nil
}

// retryTransport retries transient failures — connection errors and
// 429/5xx — with exponential backoff. Retrying POSTs is safe for this client
// because every create it issues is allow-exists idempotent and function
// invocations that change nothing mint no revisions; the worst duplicate is a
// second Release, which is inert. This exists because seeding a large org is
// exactly the load that surfaces server unavailability, and the seeder's job
// is to converge through it, not fall over.
type retryTransport struct {
	next http.RoundTripper
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	const attempts = 5
	var res *http.Response
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// 1s, 3s, 9s, 27s with jitter; bail out if the caller gave up.
			delay := time.Duration(float64(time.Second) * float64(pow3(i-1)) * (0.7 + 0.6*rand.Float64()))
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
			if req.GetBody != nil {
				body, berr := req.GetBody()
				if berr != nil {
					return res, err
				}
				req.Body = body
			} else if req.Body != nil {
				return res, err // cannot replay the body; give up
			}
		}
		res, err = t.next.RoundTrip(req)
		if err == nil && res.StatusCode != 429 && res.StatusCode < 500 {
			return res, nil
		}
		// The last attempt's response is returned as-is; closing it here would
		// hand the caller a body it cannot read.
		if res != nil && i < attempts-1 {
			res.Body.Close()
		}
	}
	return res, err
}

func pow3(n int) int {
	out := 1
	for i := 0; i < n; i++ {
		out *= 3
	}
	return out
}

// Context returns the context the client was constructed with.
func (c *Client) Context() context.Context { return c.ctx }

// Server returns the ConfigHub server URL the client talks to.
func (c *Client) Server() string { return c.server }
