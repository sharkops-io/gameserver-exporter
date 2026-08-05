package config

import "testing"

func TestParseEnvFallbackAndEnableFlags(t *testing.T) {
	env := map[string]string{
		"PALWORLD_URL": "http://pal.invalid:8212", "PALWORLD_PASSWORD": "pw", "PALWORLD_NAME": "Palworld",
		"SATISFACTORY_URL": "https://sat.invalid:7777", "SATISFACTORY_PASSWORD": "sp", "SATISFACTORY_NAME": "Satisfactory",
	}
	cfg, err := Parse(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.PalworldEnabled || !cfg.SatisfactoryEnabled {
		t.Fatal("both collectors should be enabled when their URLs are set")
	}
	if cfg.PalworldName != "Palworld" || cfg.SatisfactoryName != "Satisfactory" {
		t.Errorf("names = %q/%q", cfg.PalworldName, cfg.SatisfactoryName)
	}
}

func TestFlagBeatsEnv(t *testing.T) {
	cfg, err := Parse([]string{"--palworld.name=FromFlag"},
		func(k string) string {
			if k == "PALWORLD_URL" {
				return "http://pal.invalid:8212"
			}
			if k == "PALWORLD_PASSWORD" {
				return "pw"
			}
			if k == "PALWORLD_NAME" {
				return "FromEnv"
			}
			return ""
		})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PalworldName != "FromFlag" {
		t.Errorf("PalworldName = %q, want FromFlag", cfg.PalworldName)
	}
}

func TestURLWithoutPasswordIsAnError(t *testing.T) {
	_, err := Parse(nil, func(k string) string {
		if k == "PALWORLD_URL" {
			return "http://pal.invalid:8212"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected an error when the URL is set but the password is empty")
	}
}

func TestNoCollectorsIsAnError(t *testing.T) {
	if _, err := Parse(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected an error when nothing is configured")
	}
}

func TestInsecureSkipVerifyDefaultsOffAndFailsClosed(t *testing.T) {
	base := map[string]string{"SATISFACTORY_URL": "https://sat.invalid:7777", "SATISFACTORY_PASSWORD": "sp"}
	get := func(extra map[string]string) func(string) string {
		return func(k string) string {
			if v, ok := extra[k]; ok {
				return v
			}
			return base[k]
		}
	}

	cfg, err := Parse(nil, get(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SatisfactoryInsecure {
		t.Error("TLS verification must be ON by default")
	}

	// A typo must fail CLOSED -- verification stays on.
	cfg, _ = Parse(nil, get(map[string]string{"SATISFACTORY_INSECURE_SKIP_VERIFY": "yes"}))
	if cfg.SatisfactoryInsecure {
		t.Error(`only an explicit "true" may disable verification`)
	}

	cfg, _ = Parse(nil, get(map[string]string{"SATISFACTORY_INSECURE_SKIP_VERIFY": "true"}))
	if !cfg.SatisfactoryInsecure {
		t.Error(`"true" should disable verification`)
	}
}
