package satisfactory

import (
	"testing"

	"github.com/sharkops-io/exporter-common/metric"
)

func TestSamplesDownEmitsOnlyUp(t *testing.T) {
	got := samplesFrom(nil, "Satisfactory")
	if len(got) != 1 || got[0].Name != "gameserver_up" || got[0].Value != 0 {
		t.Fatalf("got %+v, want exactly gameserver_up = 0", got)
	}
}

func TestSamplesUp(t *testing.T) {
	got := samplesFrom(&State{
		PlayersOnline: 1, PlayersMax: 8, TechTier: 8, UptimeSeconds: 891464,
		TickRate: 29.17, Paused: false, LatencySeconds: 0.02,
	}, "Satisfactory")

	want := map[string]float64{
		"gameserver_up": 1, "gameserver_players_online": 1, "gameserver_players_max": 8,
		"gameserver_uptime_seconds": 891464, "gameserver_fps": 29.17,
		"gameserver_tech_tier": 8, "gameserver_paused": 0, "gameserver_latency_seconds": 0.02,
	}
	byName := map[string]metric.Sample{}
	for _, s := range got {
		byName[s.Name] = s
		if s.Labels["game"] != "satisfactory" || s.Labels["name"] != "Satisfactory" {
			t.Errorf("%s labels = %v", s.Name, s.Labels)
		}
	}
	for n, v := range want {
		if byName[n].Value != v {
			t.Errorf("%s = %v, want %v", n, byName[n].Value, v)
		}
	}
	if _, has := byName["gameserver_info"]; has {
		t.Error("Satisfactory reports no version; gameserver_info must not be emitted")
	}
}
