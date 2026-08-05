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

	// collector.timeout, http.timeout, log.level and log.format all default to
	// a non-empty/non-zero value, so (like
	// --satisfactory.insecure-skip-verify below) a value-based "is this still
	// the zero value" test can never tell "left at the compiled default"
	// apart from "explicitly set to that same value" -- the field already
	// holds a non-zero default before the environment is ever consulted. Each
	// gets its own IsSetByUser gate for the same reason the bool flag does.
	var timeoutSetByUser bool
	timeoutFlag := app.Flag("collector.timeout", "Per-collector timeout for a whole scrape. Keep below Prometheus' scrape_timeout.").
		Default("8s")
	timeoutFlag.IsSetByUser(&timeoutSetByUser)
	timeoutFlag.DurationVar(&cfg.Timeout)

	// Two different clocks, and both have to be raised to give a slow server
	// more time. --collector.timeout bounds a collector's whole scrape, while
	// --http.timeout bounds each individual request against that server's API.
	var httpTimeoutSetByUser bool
	httpTimeoutFlag := app.Flag("http.timeout", "Per-HTTP-request timeout. Raise this too when raising --collector.timeout.").
		Default("3s")
	httpTimeoutFlag.IsSetByUser(&httpTimeoutSetByUser)
	httpTimeoutFlag.DurationVar(&cfg.HTTPTimeout)

	var logLevel string
	var logLevelSetByUser bool
	logLevelFlag := app.Flag("log.level", "Log level: debug, info, warn or error.").Default("info")
	logLevelFlag.IsSetByUser(&logLevelSetByUser)
	logLevelFlag.EnumVar(&logLevel, "debug", "info", "warn", "error")

	var logFormatSetByUser bool
	logFormatFlag := app.Flag("log.format", "Log output format: logfmt or json.").Default("logfmt")
	logFormatFlag.IsSetByUser(&logFormatSetByUser)
	logFormatFlag.EnumVar(&cfg.LogFormat, "logfmt", "json")

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

	// Environment fallback: applied only where the flag was left at its
	// compiled default, so an explicit flag always wins. For these six,
	// "not passed" and "" are the same thing, so testing the value is
	// sufficient -- there is no separate IsSetByUser trap here because none
	// of them has a non-empty default.
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

	// COLLECTOR_TIMEOUT / HTTP_TIMEOUT: gated on IsSetByUser (see the flag
	// definitions above for why) rather than on value, since both flags
	// default to a non-zero duration. A malformed value errors rather than
	// silently keeping the compiled default, matching the flag's own
	// behaviour -- kingpin would reject a malformed --collector.timeout the
	// same way.
	if !timeoutSetByUser {
		if v := getenv("COLLECTOR_TIMEOUT"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("COLLECTOR_TIMEOUT: %w", err)
			}
			cfg.Timeout = d
		}
	}
	if !httpTimeoutSetByUser {
		if v := getenv("HTTP_TIMEOUT"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("HTTP_TIMEOUT: %w", err)
			}
			cfg.HTTPTimeout = d
		}
	}

	// LOG_LEVEL / LOG_FORMAT: same IsSetByUser gating, applied to the raw
	// string BEFORE it's validated below, so a value coming from the
	// environment is held to the same enum EnumVar would have enforced on a
	// flag.
	if !logLevelSetByUser {
		if v := getenv("LOG_LEVEL"); v != "" {
			logLevel = v
		}
	}
	if !logFormatSetByUser {
		if v := getenv("LOG_FORMAT"); v != "" {
			cfg.LogFormat = v
		}
	}

	// A flag value has already been checked by EnumVar; a value that arrived
	// via the environment has not, so this switch is the only thing
	// enforcing the enum for LOG_LEVEL now that env can supply it. An
	// unrecognised value errors rather than silently defaulting to info, so
	// a typo can't quietly discard the level someone asked for.
	switch logLevel {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return nil, fmt.Errorf("LOG_LEVEL: invalid value %q, want debug, info, warn or error", logLevel)
	}

	// Same reasoning for LOG_FORMAT: EnumVar validated a flag value; a value
	// from the environment needs the same check applied by hand.
	if cfg.LogFormat != "logfmt" && cfg.LogFormat != "json" {
		return nil, fmt.Errorf("LOG_FORMAT: invalid value %q, want logfmt or json", cfg.LogFormat)
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
	// The `name` label is load-bearing: it (together with `game`) is what
	// keeps a server's series identity stable across restarts and what a
	// dashboard's legend renders. An empty value doesn't error at the
	// Prometheus client library level -- it just emits name="" and renders a
	// blank legend entry -- so it has to be caught here instead.
	if cfg.PalworldEnabled && cfg.PalworldName == "" {
		return nil, fmt.Errorf("palworld.url is set but palworld.name is empty")
	}
	if cfg.SatisfactoryEnabled && cfg.SatisfactoryName == "" {
		return nil, fmt.Errorf("satisfactory.url is set but satisfactory.name is empty")
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
