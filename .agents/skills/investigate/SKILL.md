---
name: investigate
description: Investigate a requested code change before implementation to judge scope, readiness, and whether to split the work or run a bounded spike. Revisit when discoveries materially change those judgments.
---

# Investigate a change

Scale investigation to uncertainty; a small understood repair needs little ceremony.
Read project instructions and trace relevant code with `rg` and existing tests:

- Who consumes the changed behavior?
- Which contracts cross deployment boundaries, and in what rollout order?
- Does the capability already exist?
- What is genuinely ambiguous in the request?

Judge the evidence, not just search hits. Look for assumptions that could change
scope, design, or effort. Ask the developer about consequential behavior choices;
use judgment for technical details and coherent increments.

For stateful work, trace the consequential states, the transitions between them
(including async ordering and changes of representation) and what each transition
must preserve, before dividing the work among implementers. Stateless changes need
no inventory. For changed failure handling, trace what stops, what keeps
progressing, and how persistent failure becomes visible. When a design depends on
how an external boundary actually behaves, such as a service's output or a
browser's input handling, consider a bounded representative check within existing
permissions before freezing that assumption.

When a feature extends a boundary with retained design debt, revisit the earlier
deferral against the new change. Select a bounded improvement within existing
authority, or explain why deferral still holds and what would trigger another
look. Use the existing decision or finding record; no separate debt ledger is
needed.

Before the outer red, resolve or explicitly defer consequential questions stated
in the request. Encode decisions in tests, concise commit contracts or an active
feature contract; retain open choices in the project's finding record or that
contract. No separate question registry is needed.
Use the [review contract](../review/SKILL.md#finding-record-and-continuity) to
select or resume that record when the host has no established location.

If uncertainty prevents a sound implementation plan, run a bounded
[spike](../spike/SKILL.md) before planning the dependent work.

Briefly explain what you learned, material uncertainty, and whether to implement,
split, or spike. Keep a coherent feature together as one unit of acceptance and
final review. Split it only where every behavior that crosses the split stays in
some part's acceptance tests and final review, never to reduce its size. When
ready to implement, use [tdd](../tdd/SKILL.md) and follow existing authorization
rules.
When another session will coordinate implementation, end with a
[feature contract](../feature-contract/SKILL.md). Otherwise no plan file is needed;
no native planning mode or routine approval checkpoint is required.

## Change authority

Read the `scope:` line from `atd status` in the primary workspace at the start
of work and before consequential scope decisions. It reads repository-local Git
config shared by linked worktrees. Carry the value and its source—the
primary-workspace path where `atd status` was read—in cross-session briefs
because Git-local config may not clone. In another checkout, use the supplied
value instead of its potentially absent local setting; recheck the primary
workspace at that path before a later consequential decision. Skills stay static;
do not infer scope or set it automatically.

- `owner-controlled` grants standing coordinator authority to make worthwhile
  improvements that preserve the requested outcome, explicit boundaries and host
  constraints. This includes defect fixes, cleanup, refactors and selecting
  design-review proposals. For higher-risk refactors, state the expected benefit,
  risks and required verification. Do not use this scope for unrelated product
  redesign.
- `ticket-bounded` allows changes required to complete the requested ticket,
  including substantial verification. Add optional adjacent work only when it
  needs no substantial extra verification, manual QA, sign-off or rollout beyond
  the ticket. Escalate uncertain fit or larger burden before adding it.
- `unconfigured` or `invalid` grants no standing authority to expand scope or
  select design-review proposals. Continue work already authorized by the user's
  request or explicit delegation; ask before expanding beyond it. For invalid
  config, `atd init --scope owner-controlled` or `atd init --scope ticket-bounded`
  repairs the value.

If design review makes no proposal, configured scope lets the coordinator proceed
within that scope. With `unconfigured` or `invalid`, the user or an expressly
authorized proxy decides whether to proceed, unless existing explicit delegation
covers that decision.

This scope does not authorize launching a review, pushing, deployment or other
external action, changing the scope itself, or disregarding host constraints.
Existing explicit user or proxy delegation remains valid.
