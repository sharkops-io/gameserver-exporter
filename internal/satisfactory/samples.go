package satisfactory

import (
	"context"
	"log/slog"

	"github.com/sharkops-io/exporter-common/collector"
	"github.com/sharkops-io/exporter-common/metric"
)

// Source adapts a Client to collector.Source, emitting the house
// gameserver_* metric schema. Namespace deliberately returns "gameserver",
// not "satisfactory": the Palworld collector shares that namespace, and the
// two are disambiguated by constant labels the caller applies via
// collector.NewWithLabels.
type Source struct {
	Client *Client
	Name   string
}

var _ collector.Source = (*Source)(nil)

func (s *Source) Namespace() string { return "gameserver" }

func (s *Source) Collect(ctx context.Context) ([]metric.Sample, error) {
	// An unreachable server is gameserver_up 0, NOT an error: returning an
	// error would make the collector retract every sample including that 0,
	// and the server would go absent rather than down.
	st, err := s.Client.State(ctx)
	if err != nil {
		slog.Warn("satisfactory state query failed", "name", s.Name, "err", err)
		return samplesFrom(nil, s.Name), nil
	}
	return samplesFrom(st, s.Name), nil
}

// samplesFrom renders the house schema. A nil state means the server did not
// answer, and emits gameserver_up 0 and NOTHING else -- no stale player
// count, and (critically) no label that differs between the up and down
// states. See the Uptime% trap in the spec.
//
// There is no gameserver_info here: this API reports no version, and an
// empty-version series would be worse than omitting it.
func samplesFrom(st *State, name string) []metric.Sample {
	l := map[string]string{"game": "satisfactory", "name": name}
	if st == nil {
		return []metric.Sample{
			metric.Gauge("gameserver_up", "Whether the game server answered its status query (1) or not (0).", 0, l),
		}
	}
	return []metric.Sample{
		metric.Gauge("gameserver_up", "Whether the game server answered its status query (1) or not (0).", 1, l),
		metric.Gauge("gameserver_players_online", "Players currently connected to the game server.", float64(st.PlayersOnline), l),
		metric.Gauge("gameserver_players_max", "Maximum players the game server allows.", float64(st.PlayersMax), l),
		metric.Gauge("gameserver_uptime_seconds", "Seconds the game server has been running.", float64(st.UptimeSeconds), l),
		metric.Gauge("gameserver_fps", "Game server simulation frames per second.", st.TickRate, l),
		metric.Gauge("gameserver_tech_tier", "Current technology tier unlocked on the game server.", float64(st.TechTier), l),
		// This exists because the server runs with AUTOPAUSE enabled: while
		// paused, gameserver_uptime_seconds (game-simulation time, not
		// wall-clock) and gameserver_fps both stall. Without this metric that
		// looks identical to a stuck exporter.
		metric.Gauge("gameserver_paused",
			"Whether the game server has auto-paused its simulation (1) or not (0). While paused, gameserver_uptime_seconds and gameserver_fps stop advancing.",
			boolToFloat(st.Paused), l),
		metric.Gauge("gameserver_latency_seconds", "Status-query round-trip time in seconds.", st.LatencySeconds, l),
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
