package palworld

import (
	"testing"

	"github.com/sharkops-io/exporter-common/metric"
)

func TestSamplesUp(t *testing.T) {
	got := samplesFrom(&Status{
		Version: "v1.0.2", PlayersOnline: 3, PlayersMax: 10,
		UptimeSeconds: 24309, FPS: 51, FrameTimeMS: 19.5, LatencySeconds: 0.012,
	}, "Palworld")

	want := map[string]float64{
		"gameserver_up": 1, "gameserver_players_online": 3, "gameserver_players_max": 10,
		"gameserver_uptime_seconds": 24309, "gameserver_fps": 51,
		"gameserver_frame_time_seconds": 0.0195, "gameserver_latency_seconds": 0.012,
		"gameserver_info": 1,
	}
	byName := map[string]metric.Sample{}
	for _, s := range got {
		byName[s.Name] = s
	}
	for n, v := range want {
		s, ok := byName[n]
		if !ok {
			t.Errorf("%s missing", n)
			continue
		}
		if s.Value != v {
			t.Errorf("%s = %v, want %v", n, s.Value, v)
		}
		if s.Labels["game"] != "palworld" || s.Labels["name"] != "Palworld" {
			t.Errorf("%s labels = %v", n, s.Labels)
		}
	}
	if byName["gameserver_info"].Labels["version"] != "v1.0.2" {
		t.Error("gameserver_info must carry the version label")
	}
}

func TestSamplesDownEmitsOnlyUp(t *testing.T) {
	got := samplesFrom(nil, "Palworld")
	if len(got) != 1 {
		t.Fatalf("down server emitted %d samples, want exactly 1", len(got))
	}
	if got[0].Name != "gameserver_up" || got[0].Value != 0 {
		t.Errorf("got %s = %v, want gameserver_up = 0", got[0].Name, got[0].Value)
	}
	if _, has := got[0].Labels["version"]; has {
		t.Error("no label may differ between the up and down states")
	}
}
