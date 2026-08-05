// Package palworld talks to a Palworld dedicated server's REST API
// (RESTAPIEnabled=true), authenticated via HTTP Basic Auth using the fixed
// "admin" username and the server's admin password, and exposes a
// collector.Source that emits the house gameserver_* metric schema.
package palworld

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gitlab.com/sharkops/exporters/exporter-common/httpx"
)

// Config configures a Client.
type Config struct {
	URL      string
	Password string
	Timeout  time.Duration
}

// Client talks to a Palworld dedicated server's REST API.
type Client struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

type infoResponse struct {
	Version    string `json:"version"`
	ServerName string `json:"servername"`
}

type metricsResponse struct {
	ServerFPS        float64 `json:"serverfps"`
	CurrentPlayerNum int     `json:"currentplayernum"`
	MaxPlayerNum     int     `json:"maxplayernum"`
	Uptime           int64   `json:"uptime"`
	ServerFrameTime  float64 `json:"serverframetime"`
}

// Status is the combined server state a Source renders into samples.
type Status struct {
	Version        string
	PlayersOnline  int
	PlayersMax     int
	UptimeSeconds  int64
	FPS            float64
	FrameTimeMS    float64
	LatencySeconds float64
}

// New builds a Client from cfg. It performs no network I/O.
func New(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("palworld: URL is required")
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		password:   cfg.Password,
		httpClient: httpx.New(httpx.Options{Timeout: cfg.Timeout}),
	}, nil
}

// basicAuthHeader builds the "Authorization: Basic ..." header value for the
// fixed "admin" username, replicating what (*http.Request).SetBasicAuth does
// -- httpx.GetBytes takes a headers map rather than a *http.Request, so
// there's no request to call that method on.
func basicAuthHeader(password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+password))
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	headers := map[string]string{
		"Accept":        "application/json",
		"Authorization": basicAuthHeader(c.password),
	}
	body, err := httpx.GetBytes(ctx, c.httpClient, c.baseURL+path, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// Status fetches /v1/api/info and /v1/api/metrics and combines them into a
// single Status. LatencySeconds measures the /v1/api/metrics round-trip
// only: that field is meant to describe the server's status response, and
// folding the /v1/api/info request into it would make the number mean
// something else.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var info infoResponse
	if err := c.get(ctx, "/v1/api/info", &info); err != nil {
		return nil, err
	}

	start := time.Now()
	var m metricsResponse
	if err := c.get(ctx, "/v1/api/metrics", &m); err != nil {
		return nil, err
	}
	latency := time.Since(start).Seconds()

	return &Status{
		Version:        info.Version,
		PlayersOnline:  m.CurrentPlayerNum,
		PlayersMax:     m.MaxPlayerNum,
		UptimeSeconds:  m.Uptime,
		FPS:            m.ServerFPS,
		FrameTimeMS:    m.ServerFrameTime,
		LatencySeconds: latency,
	}, nil
}
