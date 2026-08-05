package satisfactory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Function string                                           `json:"function"`
			Data     struct{ Password, MinimumPrivilegeLevel string } `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		switch body.Function {
		case "PasswordLogin":
			*calls++
			if body.Data.Password != "clientpw" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errorCode":"wrong_password"}`))
				return
			}
			if body.Data.MinimumPrivilegeLevel != "Client" {
				t.Errorf("MinimumPrivilegeLevel = %q, want Client", body.Data.MinimumPrivilegeLevel)
			}
			w.Write([]byte(`{"data":{"authenticationToken":"tok"}}`))
		case "QueryServerState":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"errorCode":"insufficient_scope"}`))
				return
			}
			w.Write([]byte(`{"data":{"serverGameState":{"numConnectedPlayers":1,"playerLimit":8,
				"techTier":8,"totalGameDuration":891464,"isGamePaused":false,"averageTickRate":29.171138763427734}}}`))
		}
	}))
}

func TestStateParsesLivePayloadShape(t *testing.T) {
	var logins int
	srv := newTestServer(t, &logins)
	defer srv.Close()

	c, _ := New(Config{URL: srv.URL, Password: "clientpw", Timeout: time.Second, InsecureSkipVerify: true})
	got, err := c.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if got.PlayersOnline != 1 || got.PlayersMax != 8 || got.TechTier != 8 ||
		got.UptimeSeconds != 891464 || got.Paused || got.TickRate < 29.17 || got.TickRate > 29.18 {
		t.Errorf("State = %+v", got)
	}
}

func TestTokenIsCachedAcrossCalls(t *testing.T) {
	var logins int
	srv := newTestServer(t, &logins)
	defer srv.Close()

	c, _ := New(Config{URL: srv.URL, Password: "clientpw", Timeout: time.Second, InsecureSkipVerify: true})
	for i := 0; i < 3; i++ {
		if _, err := c.State(context.Background()); err != nil {
			t.Fatalf("State %d: %v", i, err)
		}
	}
	if logins != 1 {
		t.Errorf("PasswordLogin called %d times, want 1 (token must be cached)", logins)
	}
}
