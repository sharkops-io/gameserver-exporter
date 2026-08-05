package main

import (
	"testing"
	"time"

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
