# gameserver-exporter

A Prometheus exporter for two dedicated game servers: **Palworld** (REST
API) and **Satisfactory** (HTTPS API). Both collectors emit the same
`gameserver_*` metric schema, disambiguated by a `game` label
(`palworld` / `satisfactory`) and a `name` label you assign per instance.

**This repo owns the `gameserver_*` schema.** Any other exporter that wants
its game server visible next to these two must emit into this same schema
rather than inventing its own. A separate exporter, `mc-monitor`
([itzg/mc-monitor](https://github.com/itzg/mc-monitor)), covers Minecraft —
it speaks the native Minecraft server-list-ping protocol and is not part of
this repo. Its output is relabelled into this same `gameserver_*` schema at
scrape time via the `serviceMonitor.endpoints[].metricRelabelings` block in
`monitoring/mc-monitor-values.yaml`, so dashboards and alerts written against
`gameserver_*` work identically whether the underlying server is Palworld,
Satisfactory, or Minecraft.

## Quick start

```bash
docker run --rm -p 9102:9102 \
  -e PALWORLD_URL="http://<palworld-host>:8212" \
  -e PALWORLD_PASSWORD="<placeholder-admin-password>" \
  -e PALWORLD_NAME="Palworld" \
  -e SATISFACTORY_URL="https://<satisfactory-host>:7777" \
  -e SATISFACTORY_PASSWORD="<placeholder-client-password>" \
  -e SATISFACTORY_NAME="Satisfactory" \
  gameserver-exporter:local
```

Then scrape `http://localhost:9102/metrics`. Either collector can be
omitted: leaving `PALWORLD_URL` (or `SATISFACTORY_URL`) unset disables that
collector entirely, so a single instance of this exporter can run against
just a Palworld server, just a Satisfactory server, or both.

Flags always take precedence over the environment variables above, which
exist so a Helm values file can stay fully declarative.

All credentials shown in this document are placeholders. Do not commit real
hostnames, passwords, or tokens to this (public) repository.

## Flag reference

| Flag | Environment | Default | Description |
|---|---|---|---|
| `--web.listen-address` | — | `:9102` | Address to listen on. |
| `--web.telemetry-path` | — | `/metrics` | Path under which to expose metrics. |
| `--collector.timeout` | — | `8s` | Per-collector timeout for a whole scrape. Keep below Prometheus' `scrape_timeout`. |
| `--http.timeout` | — | `3s` | Per-HTTP-request timeout. **Raise this too when raising `--collector.timeout`** — see below. |
| `--log.level` | — | `info` | `debug`, `info`, `warn` or `error`. |
| `--log.format` | — | `logfmt` | `logfmt` or `json`. |
| `--collector.palworld` | — | `true` | Enable the Palworld collector. |
| `--collector.satisfactory` | — | `true` | Enable the Satisfactory collector. |
| `--palworld.url` | `PALWORLD_URL` | — | Palworld REST API base URL, e.g. `http://<host>:8212`. |
| `--palworld.password` | `PALWORLD_PASSWORD` | — | Palworld admin password. The username is always `admin` — the API has no separate username field. |
| `--palworld.name` | `PALWORLD_NAME` | — | Value of the `name` label for this Palworld server. |
| `--satisfactory.url` | `SATISFACTORY_URL` | — | Satisfactory HTTPS API base URL, e.g. `https://<host>:7777`. |
| `--satisfactory.password` | `SATISFACTORY_PASSWORD` | — | Satisfactory **Client** password. See below — this is not the Admin password. |
| `--satisfactory.name` | `SATISFACTORY_NAME` | — | Value of the `name` label for this Satisfactory server. |
| `--satisfactory.insecure-skip-verify` | `SATISFACTORY_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification for the Satisfactory server. See below. |

A collector actually runs only when it is not explicitly disabled **and**
has a URL configured — so both collectors can be left entirely unconfigured
without passing `--collector.palworld=false` / `--collector.satisfactory=false`
explicitly. If a URL is set, the matching password is required; the exporter
refuses to start otherwise rather than starting up in a state that can never
successfully scrape.

`--satisfactory.insecure-skip-verify` is the only boolean flag with an
environment fallback (`SATISFACTORY_INSECURE_SKIP_VERIFY`), and only the
literal value `true` (case-insensitive) enables it — anything else, including
a typo, is treated as `false`. An explicit flag always wins over the
environment, even `--satisfactory.insecure-skip-verify=false` against a
`true` environment variable.

Run `gameserver-exporter --help` for the same list generated straight from
the binary.

### Satisfactory: port, and which password

Two Satisfactory-specific gotchas this exporter's flags exist to route
around:

- **The API is on port 7777, not 8888.** 8888 is the game's own client/query
  port; the HTTPS management API this exporter talks to is a *separate*
  listener on 7777.
- **Use the Client password, not the Admin password.** The Satisfactory API
  requires a bearer token obtained via `PasswordLogin`, and this exporter
  requests it at `MinimumPrivilegeLevel: "Client"`. The Admin password will
  not authenticate at that privilege level.

### TLS verification defaults ON

`--satisfactory.insecure-skip-verify` defaults to `false`. The Satisfactory
server does present a self-signed certificate out of the box, so a real
deployment against an unmodified server will need this turned on — **but
that decision belongs in your deployment config (e.g. a Helm values file),
where it's visible in review, not in this exporter's defaults.** Shipping
`false` as the compiled-in default means an insecure setting can never be
silently inherited; someone has to opt in explicitly, every time.

### The two timeouts are not the same timeout

`--collector.timeout` bounds one collector's whole scrape. `--http.timeout`
bounds each individual HTTP request inside it, and a scrape can be more than
one request — the Palworld collector fetches `/v1/api/info` and
`/v1/api/metrics`, and the Satisfactory collector may need to log in (and
re-authenticate once on a stale token) before querying server state.

So raising `--collector.timeout` alone does nothing for a slow server —
every request is still cut off at `--http.timeout`. Raise both.
`--collector.timeout` should comfortably exceed `--http.timeout` × the
number of requests a scrape makes, or the collector deadline will fire
mid-sequence and the extra per-request headroom is unreachable.

### There is deliberately no `/healthz` endpoint

A liveness/readiness probe that actually collects from the game servers
would couple this pod's restart behavior to whether those servers happen to
be reachable at that instant — turning a transient network blip, or a server
restart for a mod update, into a Kubernetes-restart storm on the exporter,
which does nothing to fix the underlying problem and adds noise on top of
it. A broken exporter should surface as a `DOWN` Prometheus scrape target
(via `up` and `*_scrape_collector_success`), not as a probe result. There is
therefore nothing at `/healthz` to wire a `livenessProbe`/`readinessProbe`
to; if you need one, point it at TCP-connect on the listen port instead.

## Metric reference

Both collectors emit into the same `gameserver` namespace and are
disambiguated by the `game`/`name` labels on their series, and by a
`collector` constant label (`palworld` or `satisfactory`) on the two
self-monitoring series below.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `gameserver_up` | gauge | `game`, `name` | Whether the game server answered its status query (1) or not (0). |
| `gameserver_players_online` | gauge | `game`, `name` | Players currently connected to the game server. |
| `gameserver_players_max` | gauge | `game`, `name` | Maximum players the game server allows. |
| `gameserver_uptime_seconds` | gauge | `game`, `name` | Seconds the game server has been running. **Satisfactory only:** this is game-simulation time, not wall-clock time — see below. |
| `gameserver_fps` | gauge | `game`, `name` | Game server simulation frames/ticks per second. |
| `gameserver_frame_time_seconds` | gauge | `game`, `name` | Game server frame time in seconds. Palworld only. |
| `gameserver_tech_tier` | gauge | `game`, `name` | Current technology tier unlocked on the server. Satisfactory only. |
| `gameserver_paused` | gauge | `game`, `name` | Whether the server has auto-paused its simulation (1) or not (0). Satisfactory only — see below. |
| `gameserver_latency_seconds` | gauge | `game`, `name` | Status-query round-trip time in seconds. |
| `gameserver_info` | gauge | `game`, `name`, `version` | Game server build info; always 1, read the `version` label. Palworld only — the Satisfactory API reports no version. |
| `gameserver_scrape_collector_success` | gauge | `collector` | Whether the last collection for this collector succeeded (1) or failed (0). |
| `gameserver_scrape_collector_duration_seconds` | gauge | `collector` | Duration of the last collection for this collector, in seconds. |

An unreachable server is reported as `gameserver_up 0` and **nothing else**
— no stale player count, no label that differs between the up and down
state, and (critically) no metric-name-only field that would otherwise churn
the series identity across a restart.

### Satisfactory's uptime stalls while paused — that's what `gameserver_paused` is for

The Satisfactory deployment this exporter is built for runs with AUTOPAUSE
enabled: whenever no players are connected, the server pauses its own
simulation. While paused, both `gameserver_uptime_seconds` (which is
**game-simulation time**, not wall-clock time — it is not "seconds since the
process started") and `gameserver_fps` stop advancing entirely. Without a
separate signal, a paused-but-healthy server looks identical to a hung
exporter or a stuck game process: two flat lines. `gameserver_paused` exists
specifically to distinguish "the server is fine and just idle" from "this
metric stopped updating because something is broken."

## Building

```bash
docker build -t gameserver-exporter:local .
```

CI builds and pushes the image with
[Kaniko](https://github.com/GoogleContainerTools/kaniko) via the shared
`sharkops/gitlab-ci-common-config` `docker/build.yml` template — no
Docker-in-Docker. That builder is **single-architecture**: it produces
whatever platform the Kaniko executor itself runs on (`linux/amd64` on this
org's shared runners), not a `linux/amd64,linux/arm64` multi-arch manifest a
`docker buildx` pipeline could produce. This is an accepted trade for this
deployment — the target cluster is amd64-only — but is worth knowing if this
image is ever pulled onto arm64 hardware (e.g. a Raspberry Pi or Apple
Silicon dev machine): it won't run there without a local `docker build`.

## License

MIT. See `LICENSE`.
