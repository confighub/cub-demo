# cub-demo

A `cub` plugin that seeds a ConfigHub organization with a realistic, fleet-scale demo dataset:
**Meridian Group**, a fictional global retail / payments / logistics company with 99 Kubernetes
clusters across 8 regions, 4 environment classes and 3 departments; a platform component
catalog (cert-manager, traefik, kube-prometheus-stack, …) deployed across the fleet; 11
business workloads placed sparsely by department; their cloud resources as Crossplane managed
resources; releases, Argo-style live status with believable failures; and change workflows with
rollouts in flight. Nothing is provisioned — clusters are Targets on a server-hosted worker.

It also doubles as a scale test of ConfigHub itself.

## Install

```
cub plugin install confighub/cub-demo
cub demo version
```

`cub plugin upgrade demo` picks up new releases. To work from source instead:
`make plugin` builds and installs the local build in place.

The tool shells out to `cub` for a few operations and pairs with recent server behavior;
use a current `cub` (v0.4.16+) against a current server (v0.4.15+, where change workflows
are entities).

## Seed an org

`cub demo` acts as the **active cub context** — check `cub context get` first, and use a
dedicated org: the default scenario creates ~1,200 spaces and ~4,300 units, far beyond the
default entity quotas (the tool reports a quota error clearly and does not shrink the demo).
Ask ConfigHub to raise the org's quotas before the first full-scale run — roughly Space 2500,
Unit 10000, Link 10000 for the default scenario; on a self-hosted server, that is
`confighub admin quota set --slug <org> --entity-type Space --max 2500` and the same for
`Unit` and `Link`.

Then:

```
cub demo scenario list         # the embedded sample scenarios
cub demo plan meridian         # what a scenario expands to; offline
cub demo install meridian      # store the definition in the org (a <name>-scenario space)
cub demo up                    # converge the org to the installed definition (idempotent; re-run to resume)
cub demo status                # compare the org with the installed scenario
cub demo down                  # delete everything the scenario created
```

The split mirrors ConfigHub's own model: `install` writes the demo's *desired state* into the
org as config data, and `up` reconciles the org to it. A scenario name or file appears only on
`plan` and `install`; every other verb reads the definition back from the org, so any machine
with this plugin can operate a demo its org holds. To change a running demo, edit an exported
copy, `install` it again (the bundle units take new revisions), and `up`. If the org holds
several demos, `--demo <name>` says which one a verb operates on.

`up` runs phases in order (home, clusters, bases, deployments, releases, livestatus, views,
workflows, stories); `--phase` limits a run, `--dry-run` previews. Everything the tool creates
carries the `DemoName=<scenario>` label, which is also the teardown key.

## A short tour of the seeded org

- **The fleet**: `cub space list --where "Labels.DemoName = 'meridian' AND Labels.Layer = 'cluster'"`,
  or filter targets by facts: `cub target list --space '*' --where "Facts.Cluster.KubernetesVersion = '1.32'"`.
- **Components**: `cub component list` — 19 components, each a tree of root base → class bases
  (dev/test/uat/prod policy applied as real revisions) → per-cluster deployments. Open one in
  the UI for the flow graph with live-status chips.
- **Sparse placement**: `payment-gateway` runs on 19 clusters, `fraud-scoring` on 14; payments
  never lands on retail clusters or in South America.
- **Cloud resources**: RDS/S3/SQS/ElastiCache as Crossplane units next to their workloads, with
  per-deployment `spec.forProvider.region` and per-class instance classes.
- **Live status**: Argo vocabulary on every deployment, with a few Degraded / OutOfSync /
  Progressing exceptions and deliberately unreleased spaces
  (`cub unit list --space '*' --view meridian-platform/meridian-never-released`).
- **Rollouts**: three change orders in flight — `cert-manager-1-17-0` ready to advance to uat,
  `shipment-tracker-6-5-0` blocked at prod by a degraded uat variant, `kyverno-pinned-digests`
  fully released. `cub changeorder get cert-manager-1-17-0 --space cert-manager-base`.

## Scenarios

`scenarios/meridian.yaml` is the embedded default; `scenarios/e2e.yaml` is the small one the
e2e test uses. To customize: `cub demo scenario export meridian <dir>`, edit, then
`cub demo install <dir>/meridian.yaml` — component directories you delete fall back to the
embedded copies.
Scenario names are org-global: two scenarios must not share cluster or component names, and the
seeder refuses to adopt spaces owned by another scenario.

## Docs

- [docs/authoring-scenarios.md](docs/authoring-scenarios.md) — how to write a scenario: schema, component.yaml, templates, plays
- [docs/DESIGN.md](docs/DESIGN.md) — decisions, the product facts the seeder relies on, the pipeline
- [docs/DATA-MODEL.md](docs/DATA-MODEL.md) — what gets created: spaces, labels, slugs, facts, queries
- [docs/ROADMAP.md](docs/ROADMAP.md) — milestone history and follow-ups

## Development

```
make check         # fmt-check + vet + test (the CI gate)
CUB_DEMO_E2E_CONTEXT=<context> make e2e    # full up/status/down of the e2e scenario against the active context
```

The e2e test creates and deletes real entities in the active context's org; the guard variable
must name that context exactly.
