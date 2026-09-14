# Change workflows demo — presenter appendix

The runbook behind [change-workflows-walkthrough.md](change-workflows-walkthrough.md): setup,
reset, and the CLI form of every beat, for rehearsing without the UI or recovering mid-demo.
Verified 2026-09-12 on hub.confighub.com v0.4.15, cub v0.4.16, cub-demo main e6f76e3
(ChangeWorkflow as an entity).

## Setup

```
cub context use demo                  # the scenario is pinned to this context; the verbs refuse elsewhere
cub version                           # client and server must match
cub demo install workflows            # once per org: store the definition (re-run after upgrades)
cub demo reset --yes                  # ~40 s: teardown + seed, the opening state
cub demo play                         # the moves the scenario declares
```

`install` is needed once, for an empty org: it stores the scenario's definition in the org, and
after that every command reads it back from there, so it runs against exactly what was
installed. If the plugin was upgraded across a scenario-schema change the verbs say so and ask
for `cub demo install workflows` to refresh the stored definition.

**Opening state.** Six clusters (`us-east-dev1`, `us-east-test1/2`, `us-east-prod1/2/3`),
three components sharing one ChangeWorkflow entity, `standard-rollout` in `workflows-platform`
(`cub changeworkflow list --space workflows-platform`). catalog-api runs 5.2.0 everywhere, has no change order and no live status;
its memory limit is 512Mi everywhere except `us-east-prod1`, which carries 2Gi and protects it. cert-manager has
`cert-manager-1-17-0` landed through test with `us-east-test1` Degraded (chapter 2). Workflow
stages: `bases → dev → test (released) → prod (released, healthy)`, `final: released, healthy`.

## Chapter 1 on the CLI

| Beat | Command |
|---|---|
| The workflow | `cub changeworkflow get catalog-api-workflow --space workflows-platform` |
| CI ships | `cub demo play ship`: 5.2.0 → 5.3.0, memory 512Mi → 1Gi on the base, change order `catalog-api-base/catalog-api-5-3-0` under `workflows-platform/catalog-api-workflow` |
| Where is it | `cub changeorder list --space '*'` · `cub changeorder get catalog-api-5-3-0 --space catalog-api-base` |
| Preview bases | `cub variant promote --change-order catalog-api-base/catalog-api-5-3-0 --target-stage bases --dry-run -o mutations` — all three class bases: image, and 512Mi → 1Gi |
| Promote bases, dev | `cub variant promote --change-order catalog-api-base/catalog-api-5-3-0 --target-stage bases` then `… --target-stage dev` |
| Release gate | `cub variant promote --change-order catalog-api-base/catalog-api-5-3-0 --dry-run` → `Variant 'us-east-dev1' has taken change order … but has not released it` |
| Release dev | `cub release publish --revision ChangeOrder:catalog-api-base/catalog-api-5-3-0 catalog-api-us-east-dev1` |
| Preview test | same dry run with `-o mutations`: image and 512Mi → 1Gi on both test clusters |
| Promote, release test | `cub variant promote --change-order …` (advances to test) · `cub release publish --revision ChangeOrder:… catalog-api-us-east-test1` and `…test2` |
| Healthy gate | `… --dry-run` → `live-status not found for Variant 'us-east-test2'` |
| argobot reports | `cub demo play argobot-test` |
| Prod | preview: `prod2`/`prod3` image and memory, `prod1` image only · `cub variant promote --change-order …` · publish `catalog-api-us-east-prod{1,2,3}` · `cub demo play argobot-prod` |
| Done | `cub changeorder get …` → `State Released`, `Stage prod`, `Completed true` · `cub revision list api --space catalog-api-us-east-prod1` |

**Do not pass `--change-desc` to promote**: it replaces the pipeline's description on every
target revision and the audit trail loses its meaning.

## Chapter 2 on the CLI (draft)

```
cub variant promote --change-order cert-manager-base/cert-manager-1-17-0          # refused: us-east-test1 is not healthy
cub demo play heal-test1  # (chapter 2 will declare a cert-manager play)   # then the same promote lands
cub demo incident --component catalog-api --variant us-east-test1                # a fresh incident on demand
```

## Faking the outside world

The scenario's `plays:` block is the presenter's vocabulary; the tool only knows four
primitives a play can call:

- **`bump-image`** is the pipeline: reads the base's image tag, bumps it (`strategy: minor`).
  Nothing hardcoded, so a second run without a reset ships 5.4.0.
- **`invoke`** runs one function on one unit, with a change description.
- **`changeorder`** opens a change order under the component's workflow.
- **`observe`** is the delivery system reporting: writes Synced/Healthy on selected spaces whose
  latest release postdates their last observation, or paints an incident. Selectors: component,
  variant, stage (resolved through the component's change order in flight). Never publishes.

`cub demo play` lists the plays; `cub demo play <name> --dry-run` prints without running.

## Reset

`cub demo reset --yes`. A finished rollout cannot be re-staged in place, and a
re-run of `up` leaves completed change orders alone, so teardown is the reset.

## Version skew

The client must match the server (`cub version` shows both). A newer client fails
`changeorder list` against an older server; an older client cannot read v4 definitions.
