# AGENTS.md

Instructions for agents developing atd itself. Installing atd never copies this
file into another repository. Suggest updates when instructions conflict or drift.

## Project direction

Read [DIRECTION.md](DIRECTION.md), the authoritative project direction and map of
instruction ownership. If you believe DIRECTION is wrong, say so and ask; do not
work around it.

- Prefer deleting over deprecating. No compatibility shims, legacy flags or
  migration paths. One user, one workspace.

## Style

- Prefer self-documenting code; comments explain why, not how.
- Be concise and direct; avoid flattery. Push back on flawed reasoning,
  overlooked edge cases and questionable tradeoffs.

## Git

- Keep useful branch names short and kebab-case.
- Commit title: `<type>: imperative summary`, with `feat`, `fix`, `chore`,
  `refactor`, `test` or `docs`; no scope.
- Always ask before pushing. Do not stage or overwrite unrelated files.
- Keep machine-specific values out of tracked files.

## Tools and execution setup

- Prefer `rg` to `grep` and `fd` to `find`.
- At host setup, confirm registered tests include required wrappers and limits;
  establish targeted and mutation invocation patterns. Verify execution and ATD
  recording in the delegate's actual environment before expensive delegated tests.
  Supply the verified invocation and permissions in its brief; repeat setup when
  checkout or execution constraints change.
- ATD observations write Git objects/refs even when tests only read source. When
  sandbox-protected, use supported escalation for the complete `atd test` invocation
  before expensive tests. If approval is denied, report the blocker; do not
  substitute an unrecorded gate.

## Workflow entry points

- Begin with [investigate](.agents/skills/investigate/SKILL.md); use
  [tdd](.agents/skills/tdd/SKILL.md) for implementation and remediation, including
  it in delegate briefs. The skills own procedure and role responsibilities.
- Use the [review contract](.agents/skills/review/SKILL.md) for review readiness,
  authorization, scope, experiments, finding records and closure. Use
  [evaluate-review](.agents/skills/evaluate-review/SKILL.md) on returned findings.

## Documentation

- `docs/internals.md` describes how atd works now. When a change alters
  behavior, output, stored state or files it describes, update the affected
  sections in the same change, and rerun any transcript whose output changed.
  The full suite fails on stale citations;
  `go test -run TestInternalsCitations -update` refreshes them.

## Model routing

This table is the authoritative routing for Codex in this repository. The harness
is Codex and the provider is OpenAI for every assignment. Claude Code requires its
own separately configured and verified native routing; do not translate model
names or effort across providers.
The developer-selected session remains coordinator; do not spawn a replacement.
Instructions cannot change its running model.

| Role | Codex assignment | Work |
|---|---|---|
| Judgment | `gpt-6-sol`, `high` minimum | Exploration sufficiency, architecture, ambiguity, oracle creation, finding disposition, and final integration judgment. |
| Bounded implementation | `gpt-6-luna`, `xhigh` default/minimum | Execution with an established oracle; `max` only under the attribution policy below. |
| Independent adversarial review | `gpt-6-sol`, `high` minimum, fresh context | Authorized code or approach review, separate from the implementer and oracle author. |

A developer may explicitly override a task or session assignment with a concrete
model and effort; record it as an override, not a new default. For example,
`gpt-6-sol` at `high` is a valid explicit bounded-implementation choice even
though the Luna default uses `xhigh`. Do not rank effort labels across models.
Judgment and review floors remain unless the developer deliberately changes them.
The coordinator owns sequencing, tool execution, delegation, mechanical
verification and integration. Keep bounded work local when cheaper. A coordinator
meets the judgment route only when it satisfies both the model and minimum effort
in the table, or through a deliberate developer override for judgment. Otherwise,
it uses an explicitly routed judgment specialist for those judgments and keeps
that specialist available for later contract questions. An eligible coordinator
needs no default specialist; consult another eligible judge only for a disputed
contract that available evidence cannot settle.

- At a behavior's first message, state requested and actual model/effort and the
  confirmation source. Runtime evidence or developer confirmation is needed for
  an actual assignment; an agent's self-description is insufficient. If actual
  routing is unknown, say so.
- Delegate when expected total cost, including briefing, verification,
  corrections, retries and escalation, is lower. Retain new contracts and
  substantial repository judgment with the judgment route. Do not duplicate
  delegated implementation or take it back merely to avoid waiting. Verify
  delegated edits, test evidence, exact-tree gates and reviewed-head identity;
  an integration opinion cannot waive those checks.
- Attribute rework before increasing Luna effort: coordinator-authored oracle
  gaps require renewed coordinator investigation; delegate reasoning or
  implementation failures at `xhigh` can justify a `max` trial. Judge total cost
  and quality. Cost never lowers the judgment or independent-review minimum.
- Revisit the oracle at the judgment route when agreed behavior/assertions
  change, a mutation survives or fails inexplicably, a new consumer/deployment
  contract appears, findings are disputed, or remediation changes a coverage
  claim. Supply the relevant diff, tests, evidence and question; avoid replaying
  the session.
- For authorized implementer-prepared mutation trials, the judgment route
  selects failure scenarios and judges frozen cases; the coordinator prepares
  or delegates and executes them in isolation. Use the review contract's
  experiment evidence and restoration rules.
- A disabled, missing or unavailable delegated assignment makes that delegation
  unavailable. Report it and use only an explicitly selected alternate route;
  never silently lower capability or substitute a model from another provider.
  A qualified coordinator may still do local work under the TDD role boundaries
  and total-cost judgment above.
- Routed Codex spawns set the concrete model and effort explicitly and use
  `fork_turns: "none"` or a bounded history. Full-history forks inherit model
  and effort; use `fork_turns: "all"` only for intentional clones. Independent
  reviewers always use `fork_turns: "none"` and receive the authorized brief,
  host rules and continuity explicitly.
- Wait for blocking delegates with long `wait_agent` timeouts measured in
  minutes; completion wakes the wait. Reassess on a real timeout, without fixed
  short polling. Keep the turn active until the result arrives; do not assume
  it resumes an ended turn.
- Coordinator replacement is exceptional. After compaction, restart only at a
  durable commit/direction boundary, outside unresolved review→disposition→repair,
  and after enrichment answers are encoded in the behavior map and failing test.
  A commit alone is not a reason to restart.
