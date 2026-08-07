# gameserver-exporter — Claude Guide

Prometheus exporter for Palworld and Satisfactory. Minecraft is **not** covered
here — it comes from mc-monitor with relabeling, and the two producers share the
`gameserver_*` schema deliberately.

Full background lives on the wiki:
[`/projects/gameserver-exporter`](https://wiki.example.com/en/projects/gameserver-exporter).

## The dashboard is mirrored — editing it here changes nothing on its own

`dashboards/gameservers.json` is canonical **here**, but it is not the copy that
renders. An identical copy lives at `monitoring/dashboards/gameservers.json`
(`example-org/apps/monitoring`), and that is the one helmfile actually
reads, because `readFile` resolves against the `monitoring` repo.

So a change merged here alone is invisible in Grafana. The two copies must stay
**byte-identical**.

`monitoring`'s CI job `dashboard-drift-check` enforces it by fetching this
repo's `main` over plain HTTPS (this repo is public — no token involved). That
fetch forces a merge order:

1. Merge the MR **here** first.
2. Re-run the pipeline on the `monitoring` MR — it cannot go green before step
   1, since it compares against this repo's `main`.
3. Merge the `monitoring` MR. That is what moves the deployed bytes.

The `monitoring` pipeline failing in between is **expected and
self-correcting** — it emits one failed-pipeline email. Do not revert to
"fix" it.

The rendered ConfigMap (`cm/gameserver-exporter-gameservers` in namespace
`monitoring`) is owned by **this** project's Helm release
(`meta.helm.sh/release-name: gameserver-exporter`, `sharkops-universal` chart)
and is collected by the Grafana sidecar via the `grafana_dashboard: "1"` label.
The file is read from `monitoring`; the release that owns it is this one.

## A series with no `byName` override is silently mis-coloured

Every timeseries panel sets `color.mode: fixed` with `fixedColor: "#8E8E8E"`.
A series therefore gets a real colour **only** from an explicit `byName`
override — there is no palette fallback. One uncovered series looks fine; two
render as identical grey lines.

That is exactly how the Latency panel shipped with Palworld and Satisfactory
indistinguishable until 2026-08-07, when it was caught **by eye**, not by CI.
`monitoring`'s `test_timeseries_gets_the_by_name_palette` asserts the palette
mechanism exists on a panel, not that every series is covered by it, so the
broken panel passed identically to a correct one.

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

`stages: [test, rules, build]` — there is **no deploy stage**. Deployment
happens from the `monitoring` repo. `test` runs Go tests, lint and
`secret-scan`; `rules` runs `promtool` over `examples/rules.yaml`.

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
right" in the JSON, passes `dashboard_checks.py`, and merges cleanly can still
be the wrong dashboard, in the wrong repo, with two servers the same colour.
Check the live artifact:

```bash
kubectl get cm gameserver-exporter-gameservers -n monitoring \
  -o jsonpath='{.data.gameservers\.json}' \
  | jq -r '.panels[]|select(.title=="Latency")|.fieldConfig.overrides[]
           |"\(.matcher.options)\t\(.properties[0].value.fixedColor)"'
```
