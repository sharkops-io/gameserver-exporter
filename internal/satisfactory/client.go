// Package satisfactory talks to a Satisfactory dedicated server's HTTPS API
// (port 7777, self-signed certificate), authenticated via a bearer token
// obtained from PasswordLogin using Client privilege, and exposes a
// collector.Source that emits the house gameserver_* metric schema.
package satisfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sharkops-io/exporter-common/httpx"
)

// Config configures a Client.
type Config struct {
	URL                string
	Password           string
	Timeout            time.Duration
	InsecureSkipVerify bool
}

// Client talks to a Satisfactory dedicated server's HTTPS API, caching the
// bearer token returned by PasswordLogin across calls to State.
type Client struct {
	baseURL    string
	password   string
	httpClient *http.Client

	mu    sync.Mutex
	token string
}

// State is the server state a Source renders into samples.
type State struct {
	PlayersOnline  int
	PlayersMax     int
	TechTier       int
	UptimeSeconds  int64
	TickRate       float64
	Paused         bool
	LatencySeconds float64
}

// errUnauthorized marks a QueryServerState response that failed because the
// cached bearer token is missing, wrong, or expired (HTTP 401/403). State
// uses this, rather than the status code directly, to decide whether to
// re-authenticate and retry.
var errUnauthorized = errors.New("satisfactory: unauthorized")

type apiRequest struct {
	Function string `json:"function"`
	Data     any    `json:"data,omitempty"`
}

type passwordLoginData struct {
	Password              string `json:"Password"`
	MinimumPrivilegeLevel string `json:"MinimumPrivilegeLevel"`
}

type authResponse struct {
	Data struct {
		AuthenticationToken string `json:"authenticationToken"`
	} `json:"data"`
}

type queryServerStateResponse struct {
	Data struct {
		ServerGameState struct {
			NumConnectedPlayers int     `json:"numConnectedPlayers"`
			PlayerLimit         int     `json:"playerLimit"`
			TechTier            int     `json:"techTier"`
			TotalGameDuration   int64   `json:"totalGameDuration"`
			IsGamePaused        bool    `json:"isGamePaused"`
			AverageTickRate     float64 `json:"averageTickRate"`
		} `json:"serverGameState"`
	} `json:"data"`
}

// New builds a Client from cfg. It performs no network I/O.
func New(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("satisfactory: URL is required")
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.URL, "/"),
		password: cfg.Password,
		httpClient: httpx.New(httpx.Options{
			Timeout:            cfg.Timeout,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}),
	}, nil
}

// State queries the server, reusing a cached bearer token. A 401/403 means the
// token expired (or none has been obtained yet), so it re-authenticates
// exactly once and retries.
func (c *Client) State(ctx context.Context) (*State, error) {
	st, err := c.queryState(ctx)
	if err != nil && errors.Is(err, errUnauthorized) {
		if lerr := c.login(ctx); lerr != nil {
			return nil, lerr
		}
		return c.queryState(ctx)
	}
	return st, err
}

// login authenticates with Client privilege and caches the returned bearer
// token for subsequent calls to queryState.
func (c *Client) login(ctx context.Context) error {
	reqBody := apiRequest{
		Function: "PasswordLogin",
		Data: passwordLoginData{
			Password:              c.password,
			MinimumPrivilegeLevel: "Client",
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := httpx.ReadBodyLimited(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("satisfactory: PasswordLogin: HTTP %d: %s", resp.StatusCode, respBody)
	}

	var parsed authResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return err
	}
	if parsed.Data.AuthenticationToken == "" {
		return fmt.Errorf("satisfactory: PasswordLogin returned no token")
	}

	c.mu.Lock()
	c.token = parsed.Data.AuthenticationToken
	c.mu.Unlock()
	return nil
}

// queryState issues QueryServerState with whatever bearer token is currently
// cached (possibly none, on the very first call). LatencySeconds measures
// only this round-trip -- deliberately excluding PasswordLogin, so the
// latency metric doesn't spike on exactly the scrapes where the cached token
// had expired, which is a property of the exporter's caching, not the
// server's responsiveness.
func (c *Client) queryState(ctx context.Context) (*State, error) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	body, err := json.Marshal(apiRequest{Function: "QueryServerState"})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	latency := time.Since(start).Seconds()

	respBody, err := httpx.ReadBodyLimited(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("satisfactory: QueryServerState: HTTP %d: %s", resp.StatusCode, respBody)
	}

	var parsed queryServerStateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	gs := parsed.Data.ServerGameState
	return &State{
		PlayersOnline:  gs.NumConnectedPlayers,
		PlayersMax:     gs.PlayerLimit,
		TechTier:       gs.TechTier,
		UptimeSeconds:  gs.TotalGameDuration,
		TickRate:       gs.AverageTickRate,
		Paused:         gs.IsGamePaused,
		LatencySeconds: latency,
	}, nil
}
