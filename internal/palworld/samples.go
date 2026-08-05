package palworld

import (
	"context"
	"log/slog"

	"gitlab.com/sharkops/exporters/exporter-common/collector"
	"gitlab.com/sharkops/exporters/exporter-common/metric"
)

// Source adapts a Client to collector.Source, emitting the house
// gameserver_* metric schema. Namespace deliberately returns "gameserver",
// not "palworld": the Satisfactory collector shares that namespace, and the
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
	st, err := s.Client.Status(ctx)
	if err != nil {
		slog.Warn("palworld status query failed", "name", s.Name, "err", err)
		return samplesFrom(nil, s.Name), nil
	}
	return samplesFrom(st, s.Name), nil
}

// samplesFrom renders the house schema. A nil status means the server did not
// answer, and emits gameserver_up 0 and NOTHING else -- no stale player count,
// and (critically) no label that differs between the up and down states. See
// the Uptime% trap in the spec.
func samplesFrom(st *Status, name string) []metric.Sample {
	l := map[string]string{"game": "palworld", "name": name}
	if st == nil {
		return []metric.Sample{
			metric.Gauge("gameserver_up", "Whether the game server answered its status query (1) or not (0).", 0, l),
		}
	}
	out := []metric.Sample{
		metric.Gauge("gameserver_up", "Whether the game server answered its status query (1) or not (0).", 1, l),
		metric.Gauge("gameserver_players_online", "Players currently connected to the game server.", float64(st.PlayersOnline), l),
		metric.Gauge("gameserver_players_max", "Maximum players the game server allows.", float64(st.PlayersMax), l),
		metric.Gauge("gameserver_uptime_seconds", "Seconds the game server has been running.", float64(st.UptimeSeconds), l),
		metric.Gauge("gameserver_fps", "Game server simulation frames per second.", st.FPS, l),
		// Base units: the API reports milliseconds.
		metric.Gauge("gameserver_frame_time_seconds", "Game server frame time in seconds.", st.FrameTimeMS/1000, l),
		metric.Gauge("gameserver_latency_seconds", "Status-query round-trip time in seconds.", st.LatencySeconds, l),
	}
	if st.Version != "" {
		out = append(out, metric.Gauge("gameserver_info", "Game server build info; always 1, read the labels.", 1,
			map[string]string{"game": "palworld", "name": name, "version": st.Version}))
	}
	return out
}
