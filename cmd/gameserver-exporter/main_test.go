package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"gitlab.com/sharkops/exporters/gameserver-exporter/internal/config"
)

func TestBuildRegistryEmitsBothCollectorsSelfMonitoring(t *testing.T) {
	cfg := &config.Config{
		Timeout: time.Second, HTTPTimeout: time.Second,
		PalworldEnabled: true, PalworldURL: "http://127.0.0.1:1", PalworldPassword: "x", PalworldName: "Palworld",
		SatisfactoryEnabled: true, SatisfactoryURL: "https://127.0.0.1:1", SatisfactoryPassword: "x",
		SatisfactoryName: "Satisfactory", SatisfactoryInsecure: true,
	}
	reg, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	// Nothing is listening on port 1, so both collectors fail -- which is
	// exactly the down path. Gather must still succeed.
	got, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather must not fail when collectors fail: %v", err)
	}

	counts := map[string]int{}
	for _, mf := range got {
		counts[mf.GetName()] = len(mf.GetMetric())
	}
	if counts["gameserver_scrape_collector_success"] != 2 {
		t.Errorf("scrape_collector_success series = %d, want 2 (one per collector)",
			counts["gameserver_scrape_collector_success"])
	}
	if counts["gameserver_up"] != 2 {
		t.Errorf("gameserver_up series = %d, want 2", counts["gameserver_up"])
	}
}

// newTestPalworldServer reproduces the payload shape asserted by
// internal/palworld/client_test.go's TestStatusParsesBothEndpoints.
func newTestPalworldServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != "testpw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/api/info":
			w.Write([]byte(`{"version":"v1.0.2.101103","servername":"Wild Nest"}`))
		case "/v1/api/metrics":
			w.Write([]byte(`{"serverfps":51,"currentplayernum":3,"maxplayernum":10,"uptime":24309,"serverframetime":19.5}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// newTestSatisfactoryServer reproduces the payload shape and login flow
// asserted by internal/satisfactory/client_test.go's newTestServer helper.
func newTestSatisfactoryServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Function string                                           `json:"function"`
			Data     struct{ Password, MinimumPrivilegeLevel string } `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		switch body.Function {
		case "PasswordLogin":
			if body.Data.Password != "clientpw" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errorCode":"wrong_password"}`))
				return
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

// TestBuildRegistryUpPathHasBothGamesInEverySharedFamily is the up-path
// counterpart to TestBuildRegistryEmitsBothCollectorsSelfMonitoring above,
// which only ever exercises the down path (nothing listening on port 1),
// where gameserver_up is the ONLY shared metric family in play. That leaves
// a real gap: client_golang requires every metric within a family to carry
// an identical HELP string, and if a HELP string ever diverged between the
// two collectors for one of the other shared families (gameserver_fps,
// gameserver_uptime_seconds, etc.), Gather would fail on just that family
// and, under this exporter's ContinueOnError handling, silently serve
// everything else at HTTP 200 with that one game's series absent rather
// than reporting an error anywhere. This test stands up both game servers
// for real (see the deliberate-failure check in the review report for
// verification that it actually catches that class of bug) and asserts
// every shared family carries both games' series.
func TestBuildRegistryUpPathHasBothGamesInEverySharedFamily(t *testing.T) {
	palSrv := newTestPalworldServer(t)
	defer palSrv.Close()
	satSrv := newTestSatisfactoryServer(t)
	defer satSrv.Close()

	cfg := &config.Config{
		Timeout: 5 * time.Second, HTTPTimeout: 5 * time.Second,
		PalworldEnabled: true, PalworldURL: palSrv.URL, PalworldPassword: "testpw", PalworldName: "Palworld",
		SatisfactoryEnabled: true, SatisfactoryURL: satSrv.URL, SatisfactoryPassword: "clientpw",
		SatisfactoryName: "Satisfactory", SatisfactoryInsecure: true,
	}
	reg, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	got, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather must not fail on the up path: %v", err)
	}

	families := map[string]*dto.MetricFamily{}
	for _, mf := range got {
		families[mf.GetName()] = mf
	}

	shared := []string{
		"gameserver_up", "gameserver_players_online", "gameserver_players_max",
		"gameserver_uptime_seconds", "gameserver_fps", "gameserver_latency_seconds",
	}
	for _, name := range shared {
		mf, ok := families[name]
		if !ok {
			t.Errorf("family %s is entirely absent", name)
			continue
		}
		games := gameLabelValues(mf)
		if !games["palworld"] {
			t.Errorf("family %s is missing the palworld series", name)
		}
		if !games["satisfactory"] {
			t.Errorf("family %s is missing the satisfactory series", name)
		}
	}
}

// gameLabelValues collects the set of `game` label values present across a
// gathered metric family's series.
func gameLabelValues(mf *dto.MetricFamily) map[string]bool {
	out := map[string]bool{}
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "game" {
				out[l.GetValue()] = true
			}
		}
	}
	return out
}
