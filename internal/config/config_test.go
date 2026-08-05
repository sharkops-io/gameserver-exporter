package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

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
	base := map[string]string{
		"SATISFACTORY_URL": "https://sat.invalid:7777", "SATISFACTORY_PASSWORD": "sp", "SATISFACTORY_NAME": "Satisfactory",
	}
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

	// An EXPLICIT false must beat the environment. kingpin bool flags don't
	// accept "--flag=false" (that's a parse error, "unexpected false" --
	// verified directly against kingpin v2.4.0); the negation form
	// "--no-<flag>" is the actual way to pass an explicit false on the
	// command line.
	cfg, err = Parse([]string{"--no-satisfactory.insecure-skip-verify"},
		get(map[string]string{"SATISFACTORY_INSECURE_SKIP_VERIFY": "true"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SatisfactoryInsecure {
		t.Error("an explicit --no-satisfactory.insecure-skip-verify must beat the environment")
	}

	// And an explicit --flag=true still works with no env set.
	cfg, err = Parse([]string{"--satisfactory.insecure-skip-verify"}, get(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.SatisfactoryInsecure {
		t.Error("an explicit --satisfactory.insecure-skip-verify should enable it")
	}
}

// baseValidEnv is a minimal set of environment variables that parses
// successfully (Palworld enabled, nothing else configured), so the tests
// below can layer one extra variable on top without tripping unrelated
// validation.
func baseValidEnv() map[string]string {
	return map[string]string{
		"PALWORLD_URL": "http://pal.invalid:8212", "PALWORLD_PASSWORD": "pw", "PALWORLD_NAME": "Palworld",
	}
}

func getenvFromMap(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestCollectorAndHTTPTimeoutEnvFallback(t *testing.T) {
	env := baseValidEnv()
	env["COLLECTOR_TIMEOUT"] = "30s"
	env["HTTP_TIMEOUT"] = "10s"

	cfg, err := Parse(nil, getenvFromMap(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s (from COLLECTOR_TIMEOUT)", cfg.Timeout)
	}
	if cfg.HTTPTimeout != 10*time.Second {
		t.Errorf("HTTPTimeout = %v, want 10s (from HTTP_TIMEOUT)", cfg.HTTPTimeout)
	}
}

func TestCollectorAndHTTPTimeoutFlagBeatsEnv(t *testing.T) {
	env := baseValidEnv()
	env["COLLECTOR_TIMEOUT"] = "30s"
	env["HTTP_TIMEOUT"] = "10s"

	cfg, err := Parse([]string{"--collector.timeout=5s", "--http.timeout=2s"}, getenvFromMap(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s (flag should beat COLLECTOR_TIMEOUT env)", cfg.Timeout)
	}
	if cfg.HTTPTimeout != 2*time.Second {
		t.Errorf("HTTPTimeout = %v, want 2s (flag should beat HTTP_TIMEOUT env)", cfg.HTTPTimeout)
	}
}

func TestMalformedTimeoutEnvIsAnError(t *testing.T) {
	env := baseValidEnv()
	env["COLLECTOR_TIMEOUT"] = "not-a-duration"
	if _, err := Parse(nil, getenvFromMap(env)); err == nil {
		t.Fatal("expected an error for a malformed COLLECTOR_TIMEOUT")
	}

	env = baseValidEnv()
	env["HTTP_TIMEOUT"] = "not-a-duration"
	if _, err := Parse(nil, getenvFromMap(env)); err == nil {
		t.Fatal("expected an error for a malformed HTTP_TIMEOUT")
	}
}

func TestLogLevelAndFormatEnvFallback(t *testing.T) {
	env := baseValidEnv()
	env["LOG_LEVEL"] = "debug"
	env["LOG_FORMAT"] = "json"

	cfg, err := Parse(nil, getenvFromMap(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug (from LOG_LEVEL)", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json (from LOG_FORMAT)", cfg.LogFormat)
	}
}

func TestLogLevelAndFormatFlagBeatsEnv(t *testing.T) {
	env := baseValidEnv()
	env["LOG_LEVEL"] = "debug"
	env["LOG_FORMAT"] = "json"

	cfg, err := Parse([]string{"--log.level=error", "--log.format=logfmt"}, getenvFromMap(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.LogLevel != slog.LevelError {
		t.Errorf("LogLevel = %v, want error (flag should beat LOG_LEVEL env)", cfg.LogLevel)
	}
	if cfg.LogFormat != "logfmt" {
		t.Errorf("LogFormat = %q, want logfmt (flag should beat LOG_FORMAT env)", cfg.LogFormat)
	}
}

func TestInvalidLogLevelEnvIsAnError(t *testing.T) {
	env := baseValidEnv()
	env["LOG_LEVEL"] = "verbose"
	_, err := Parse(nil, getenvFromMap(env))
	if err == nil {
		t.Fatal("expected an error for an invalid LOG_LEVEL")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Errorf("error = %v, want it to mention LOG_LEVEL", err)
	}
}

func TestInvalidLogFormatEnvIsAnError(t *testing.T) {
	env := baseValidEnv()
	env["LOG_FORMAT"] = "xml"
	_, err := Parse(nil, getenvFromMap(env))
	if err == nil {
		t.Fatal("expected an error for an invalid LOG_FORMAT")
	}
	if !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Errorf("error = %v, want it to mention LOG_FORMAT", err)
	}
}

func TestNameRequiredWhenCollectorEnabled(t *testing.T) {
	env := map[string]string{"PALWORLD_URL": "http://pal.invalid:8212", "PALWORLD_PASSWORD": "pw"}
	_, err := Parse(nil, getenvFromMap(env))
	if err == nil {
		t.Fatal("expected an error when palworld.url is set but palworld.name is empty")
	}

	env = map[string]string{"SATISFACTORY_URL": "https://sat.invalid:7777", "SATISFACTORY_PASSWORD": "sp"}
	_, err = Parse(nil, getenvFromMap(env))
	if err == nil {
		t.Fatal("expected an error when satisfactory.url is set but satisfactory.name is empty")
	}
}
