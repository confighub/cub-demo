# Roadmap

Milestones are independently mergeable and each leaves something visible in a ConfigHub org.
Update the status column when a milestone lands; this file is the coordination point for
whoever picks the work up next.

| # | Milestone | Visible outcome | Depends on | Status |
|---|---|---|---|---|
| 0 | Repo, plugin skeleton, these docs | `cub demo version` works after `make plugin`; the design is in the repo | — | **done** 2026-08-29 |
| 1 | Scenario schema, expansion, `plan`, `scenario list/show/export` | `cub demo plan` prints the Meridian fleet offline; expansion has unit tests | 0 | **done** 2026-08-29 — 99 clusters, 19 components, 1229 spaces, 4317 units |
| 2 | Phases `home` + `clusters`; `down`; `status` | ~100 clusters with facts in the UI; `cub target list --where "Facts.Cluster.KubernetesVersion = '1.32'"` | 1 | **done** 2026-08-30 — Meridian's 99 clusters live in a dedicated demo org on hub.confighub.com; up takes ~4s |
| 3 | Phases `bases` + `deployments`, with two catalog components | component trees and the deployment matrix; `cub component list` | 2 | **done** 2026-08-30 — cert-manager + traefik across all 99 clusters (198 deployment spaces, 936 units, ~80s); tuned against hub.confighub.com directly |
| 4 | Releases, live status, views | a green fleet with a few red spots; the `never-released` view | 3 | **done** 2026-08-30 — 194 releases, live status on all 198 deployments (3 Degraded/2 OutOfSync/3 Progressing among them), 5 filters+views, cert-manager image skew in eu-west prod |
| 5 | Full content: eight catalog components, eleven workloads, Crossplane resources, region and cluster functions | the whole Meridian dataset; `configboard` lights up | 3 | |
| 6 | Workflows and ChangeOrder stories | rollouts in flight, one blocked. **Gated on the product specifying multi-level stages**; until then the scenario's per-level workflow template is marked temporary | 4, product | blocked |
| 7 | README tour, CI, repo + install path | clonable; installable via `make plugin`, later `cub plugin install confighub/cub-demo` | 4 | **done** 2026-08-31 — started as an internal repo with path-based install; moved to confighub/cub-demo with release-based install 2026-09-14 |

| 8 | Multi-demo support: per-scenario context pin, configurable stage prerequisites, provenance (scenario bundle stored in the target org), `cub demo list`; the `workflows` scenario (6 clusters, 3 components, per-stage gates differing) for demoing change workflows in the `demo` org | two demos in two orgs without footguns; every org self-describing | 7 | **done** 2026-09-01 — the workflows scenario moved out of the repo 2026-09-16 (demo content, maintained with the demo; installed by path) |

Milestones 5 and 6 are independent of each other.

## Default scenario: Meridian

A fictional global retail / payments / logistics group, all on AWS.

- **Regions (8):** us-east, us-west, eu-central, eu-west, eu-north, ap-southeast, ap-northeast,
  sa-east.
- **Classes (4):** dev, test, uat, prod — replicas 1/1/2/3, log level debug/debug/info/warn,
  resource size S/S/M/L, RDS class per class, prod multi-AZ.
- **Departments (4):** shared, retail, payments, logistics.
- **Clusters:** ≈99, from per-class × per-department rules with per-region scale-out counts
  (prod ≈ 52 of them). Kubernetes versions 1.30/1.31/1.32 assigned deterministically.
- **Catalog (8):** cert-manager, external-dns, external-secrets, traefik, kube-prometheus-stack
  (trimmed), fluent-bit, kyverno (test and up), velero (uat and up). Four to six units each,
  including the CRs that carry the business-specific configuration.
- **Workloads (11):** retail — storefront, catalog-api (+RDS), cart (+ElastiCache), checkout
  (+RDS, +SQS), search (+S3); payments — payment-gateway (+RDS multi-AZ), ledger (+RDS),
  fraud-scoring (+S3); logistics — shipment-tracker (+RDS, +SQS), warehouse-api (+RDS);
  shared — identity (+RDS). Placed on the owning department's clusters plus shared dev/test,
  minus per-workload region exclusions, so the component × cluster matrix is sparse.
- **Story:** ~3 % of deployments Degraded or OutOfSync, ~2 % Progressing, ~2 % with unreleased
  changes, one regional image skew, three ChangeOrders at different stages (milestone 6).
- **Size:** roughly 1,150 Spaces, 5,000 units, 5,000 links, 1,000 Releases.

## Follow-ups

- A presenter cheat-sheet verb (working name `cub demo brief`): read-only summary of the
  living org's narrative state — which rollouts are in flight and what their next gate waits
  on, which spaces are degraded — so a presenter can walk up to an org another demo moved
  and know where the stories stand. The full-scale demo script for meridian gets rewritten
  against the play model alongside it.
- `cub resource list --view` does not apply the view's attached Filter and leaves
  `Space.Labels.*` metadata columns blank; the Resource table also serves duplicate rows (a
  stale blank twin per resource) — raised 2026-09-02, issues pending.
- The value-recording-triggers experiment was removed in favor of Resource views (Jesper's
  practice); meridian retains recorded Values on old revisions as inert history.

## Follow-ups outside this repo

- The product docs' tutorial has a TODO for a fleet-scale dataset; point it here once
  milestone 4 lands.
- The multi-level-stage requirement for change workflows belongs with the change-workflows
  design in the product repo.
