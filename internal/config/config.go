// Package config binds this exporter's flags, each with an environment
// fallback so a Helm values file can stay declarative.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
)

// ErrHelpRequested is returned by Parse when the user passed --help. It is
// not a configuration error — kingpin has already written the usage text to
// its usage writer — so a caller must check for it with errors.Is and exit 0,
// rather than treating it like every other Parse error (which means "print
// this and exit 1").
//
// This exists because Parse calls app.Terminate(nil): kingpin's own
// os.Exit(0)-on-help behavior is disabled on purpose, so that Parse stays
// callable from a test without ending the test binary's process. With
// terminate turned into a no-op, kingpin's Parse falls through --help
// exactly like an empty command line and returns a zero-value Config with a
// nil error — indistinguishable, to a caller, from "the user ran this with
// no flags at all", which cmd/gameserver-exporter's "no collectors enabled"
// check correctly treats as a fatal misconfiguration. That check is
// load-bearing (an exporter with nothing configured must fail loudly rather
// than serve an empty /metrics forever), so it cannot simply be relaxed;
// --help needs its own, distinct signal instead.
var ErrHelpRequested = errors.New("help requested")

type Config struct {
	ListenAddress string
	TelemetryPath string
	Timeout       time.Duration

	LogLevel  slog.Level
	LogFormat string

	PalworldEnabled  bool
	PalworldURL      string
	PalworldPassword string
	PalworldName     string

	SatisfactoryEnabled  bool
	SatisfactoryURL      string
	SatisfactoryPassword string
	SatisfactoryName     string
	SatisfactoryInsecure bool

	HTTPTimeout time.Duration
}

// LogHandler builds the slog handler --log.level and --log.format select.
// "logfmt" maps to slog's TextHandler, whose key=value output is the logfmt
// the Prometheus toolkit's own flag of this name produces.
func (c *Config) LogHandler(w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "json" {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// Parse builds a Config from args and the environment. getenv is injected so
// the precedence rules are testable without touching the real environment.
func Parse(args []string, getenv func(string) string) (*Config, error) {
	app := kingpin.New("gameserver-exporter", "Prometheus exporter for Palworld and Satisfactory dedicated servers.")
	app.Terminate(nil)

	// Track --help explicitly (see ErrHelpRequested) rather than relying on
	// kingpin's own exit behavior, which is disabled above.
	var helpRequested bool
	app.HelpFlag.IsSetByUser(&helpRequested)

	var cfg Config

	app.Flag("web.listen-address", "Address to listen on.").Default(":9102").StringVar(&cfg.ListenAddress)
	app.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").StringVar(&cfg.TelemetryPath)
	app.Flag("collector.timeout", "Per-collector timeout for a whole scrape. Keep below Prometheus' scrape_timeout.").
		Default("8s").DurationVar(&cfg.Timeout)
	// Two different clocks, and both have to be raised to give a slow server
	// more time. --collector.timeout bounds a collector's whole scrape, while
	// --http.timeout bounds each individual request against that server's API.
	app.Flag("http.timeout", "Per-HTTP-request timeout. Raise this too when raising --collector.timeout.").
		Default("3s").DurationVar(&cfg.HTTPTimeout)

	var logLevel string
	app.Flag("log.level", "Log level: debug, info, warn or error.").Default("info").
		EnumVar(&logLevel, "debug", "info", "warn", "error")
	app.Flag("log.format", "Log output format: logfmt or json.").Default("logfmt").
		EnumVar(&cfg.LogFormat, "logfmt", "json")

	palOn := app.Flag("collector.palworld", "Enable the Palworld collector.").Default("true").Bool()
	satOn := app.Flag("collector.satisfactory", "Enable the Satisfactory collector.").Default("true").Bool()

	app.Flag("palworld.url", "Palworld REST API base URL.").StringVar(&cfg.PalworldURL)
	app.Flag("palworld.password", "Palworld admin password (username is always \"admin\").").StringVar(&cfg.PalworldPassword)
	app.Flag("palworld.name", "Value of the `name` label for this Palworld server.").StringVar(&cfg.PalworldName)

	app.Flag("satisfactory.url", "Satisfactory HTTPS API base URL (port 7777, NOT 8888).").StringVar(&cfg.SatisfactoryURL)
	app.Flag("satisfactory.password", "Satisfactory CLIENT password. Not the admin password.").StringVar(&cfg.SatisfactoryPassword)
	app.Flag("satisfactory.name", "Value of the `name` label for this Satisfactory server.").StringVar(&cfg.SatisfactoryName)
	// Defaults to FALSE, deliberately. The Satisfactory server does present a
	// self-signed cert, so this must be turned on for our deployment -- but it
	// is turned on in the values file, where it is visible in review, rather
	// than defaulted here where it would be an insecure default wearing a
	// flag's clothing. another-app hardcodes InsecureSkipVerify: true with no
	// way to turn it off; this is strictly better than the behaviour it
	// replaces. Verifying the server's cert properly (pinning its CA) is a
	// reasonable future improvement and is not in scope here.
	var insecureSetByUser bool
	insecureFlag := app.Flag("satisfactory.insecure-skip-verify",
		"Skip TLS verification for the Satisfactory server, whose certificate is self-signed.").Default("false")
	insecureFlag.IsSetByUser(&insecureSetByUser)
	insecureFlag.BoolVar(&cfg.SatisfactoryInsecure)

	if _, err := app.Parse(args); err != nil {
		return nil, err
	}
	if helpRequested {
		return nil, ErrHelpRequested
	}

	// EnumVar already rejected anything else, so this cannot fail silently
	// into a wrong level.
	switch logLevel {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		cfg.LogLevel = slog.LevelInfo
	}

	// Environment fallback: applied only where the flag left the value empty,
	// so a flag always wins.
	fallback(&cfg.PalworldURL, getenv("PALWORLD_URL"))
	fallback(&cfg.PalworldPassword, getenv("PALWORLD_PASSWORD"))
	fallback(&cfg.PalworldName, getenv("PALWORLD_NAME"))
	fallback(&cfg.SatisfactoryURL, getenv("SATISFACTORY_URL"))
	fallback(&cfg.SatisfactoryPassword, getenv("SATISFACTORY_PASSWORD"))
	fallback(&cfg.SatisfactoryName, getenv("SATISFACTORY_NAME"))

	// Bool env fallback needs its own helper -- `fallback` only handles
	// strings, and a bool has no "empty" value to test.
	//
	// Gate on IsSetByUser, NOT on the value. BoolVar cannot distinguish "flag
	// omitted" from "flag explicitly passed as =false" -- both leave the field
	// at its zero value -- so testing `!cfg.SatisfactoryInsecure` lets the
	// environment override an explicit
	// `--satisfactory.insecure-skip-verify=false`. That breaks flags-beat-env
	// AND fails OPEN. The same file already uses IsSetByUser for --help.
	//
	// Only an explicit "true" enables it, so a typo ("yes", "1") fails closed.
	if !insecureSetByUser && strings.EqualFold(getenv("SATISFACTORY_INSECURE_SKIP_VERIFY"), "true") {
		cfg.SatisfactoryInsecure = true
	}

	// A collector runs when it is not disabled AND has somewhere to connect
	// to, so either half can be left unconfigured without a flag.
	cfg.PalworldEnabled = *palOn && cfg.PalworldURL != ""
	cfg.SatisfactoryEnabled = *satOn && cfg.SatisfactoryURL != ""

	if cfg.PalworldEnabled && cfg.PalworldPassword == "" {
		return nil, fmt.Errorf("palworld.url is set but palworld.password is empty")
	}
	if cfg.SatisfactoryEnabled && cfg.SatisfactoryPassword == "" {
		return nil, fmt.Errorf("satisfactory.url is set but satisfactory.password is empty")
	}
	if !cfg.PalworldEnabled && !cfg.SatisfactoryEnabled {
		return nil, errors.New("no collectors enabled; set --palworld.url and/or --satisfactory.url")
	}

	return &cfg, nil
}

func fallback(dst *string, env string) {
	if *dst == "" {
		*dst = env
	}
}
