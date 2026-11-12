# gameserver-exporter — Claude Guide

Prometheus exporter for Palworld and Satisfactory. Minecraft is **not** covered
here — it comes from mc-monitor with relabeling, and the two producers share the
`gameserver_*` schema deliberately.

## A series with no `byName` override is silently mis-coloured

Every timeseries panel in `dashboards/gameservers.json` sets
`color.mode: fixed` with `fixedColor: "#8E8E8E"`. A series therefore gets a
real colour **only** from an explicit `byName` override — there is no palette
fallback. One uncovered series looks fine; two render as identical grey lines.

That is exactly how the Latency panel shipped with Palworld and Satisfactory
indistinguishable until 2026-08-07, when it was caught **by eye**, not by CI.
A check that only asserts the palette mechanism exists on a panel, rather than
that every series is covered by it, passes a broken panel identically to a
correct one.

One server, one colour, everywhere:

| Server | Colour |
|---|---|
| Cozy Land | `#3583B4` |
| Production | `#B34C00` |
| Creative | `#00916A` |
| Palworld | `#A0459B` |
| Satisfactory | `#B57D00` |

Panels that cover only *some* servers are deliberate, not gaps: `Simulation
rate` and `Server uptime` (Palworld + Satisfactory), `Frame time` (Palworld
only). Satisfactory reports no frame time, and mc-monitor cannot see inside a
Minecraft server, so those series never exist for the others.

When adding a server, add its override to **every** panel — not just the one
you happened to be looking at.

## CI

`.github/workflows/ci.yml` calls the shared `sharkops-io/.github`
`exporter.yml` workflow.

Traps this repo has already paid for:

- **GitLab CI *appends* its shell to a container `ENTRYPOINT`** rather than
  replacing it, so an image whose entrypoint is a binary needs
  `entrypoint: [""]` in the job. `docker run --entrypoint sh` does **not**
  prove the job works — it overrides the exact thing that breaks.
- **kingpin v2 rejects `--flag=false`** (`unexpected false`). The negative form
  is `--no-<flag>`.
- **`BoolVar` cannot distinguish an omitted flag from an explicit `false`**, so
  an env-var fallback for a flag with a non-empty default must gate on
  `IsSetByUser`, or the env var overrides an explicit `--no-<flag>` and fails
  open.

## Rendering is not verification

Every serious defect in this project rendered perfectly and failed only at
deploy time, or never announced itself at all. A dashboard edit that "looks
right" in the JSON and merges cleanly can still put two servers in the same
colour. Check the dashboard as Grafana actually renders it, not just the JSON.
