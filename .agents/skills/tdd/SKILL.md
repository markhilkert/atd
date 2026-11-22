---
name: tdd
description: Implement or remediate understood behavior through outside-in TDD with delegated implementation.
---

# TDD

## Roles

The coordinator establishes the contract and outer red, judges test sufficiency,
and verifies integration. When a [feature contract](../feature-contract/SKILL.md)
records the agreed behavior, work from it and still write the outer red. Brief the
implementer with this skill, acceptance tests, expectations, boundaries, test
commands, and the current `scope:` value and source from primary-workspace
`atd status`. The implementer must use this supplied value if local Git config is
absent and recheck the primary workspace before later consequential decisions.

The implementer owns production edits, focused tests, debugging and refactoring;
individual inner tests need no approval. Escalate new behavior decisions, changed
or weakened agreed assertions, new consumer/deployment contracts, changed coverage
claims, or consequential design problems shared across boundaries before further
slices can compound them. The coordinator resolves uncertainty and lets the
implementer continue without duplicating the work. Routine refactoring remains
implementer-owned and needs no per-extraction approval.

Choose implementation work, including defect fixes, cleanup and refactors, under
[change authority](../investigate/SKILL.md#change-authority). Escalate work beyond
that authority before making it.

## Test validity

Derive expectations from agreed behavior and actual consumers. Trace production
producers, consumers and operations bypassed by fakes; state coverage limits. Identify
plausible wrong behavior assertions should reject, valid behavior to preserve,
and consequential ordering across waits or persisted state. Assert the agreed
semantic outcome; do not reject wording or status labels unless the contract requires
them.

When delivering a consumer workflow, exercise its documented entry point using
the artifacts and setup available to that consumer. Identify extra setup or
knowledge supplied by the test; do not let a convenient test invocation silently
substitute for the delivered handoff. Scale this check to the changed workflow,
without requiring a separate usability review.

When introducing or changing a focused test filter, confirm the executed package
and full test/subtest identities match the intended selection using runner-native
execution output. Zero or unexpected selection is inconclusive behavioral evidence
even when the command exits 0; listing alone is not execution.

## Cycle

1. Write an acceptance test for one small behavior through its consumer boundary.
   Named acceptance tests are the behavior map; assertions express contracts.
   Confirm the intended failure and normally commit the outer red before implementation;
   investigate unexpected green or unrelated failures without manufacturing red.
2. Work inward through focused red/green/refactor cycles as needed: observe the
   failure, implement the missing behavior, then refactor with relevant tests green.
   The outer test may remain red. Treat testing difficulty as design feedback;
   avoid tests that merely repeat implementation.
3. Return to the outer test to find integration gaps. Once green, refactor across
   boundaries and run required verification using project wrappers and limits.
4. At feature completion, return the cumulative diff, test evidence and
   validation, principal design changes, material retained tradeoffs and limits,
   and unresolved design debt. The coordinator verifies implementation readiness
   and arranges the separate design review below before declaring the feature
   ready for final adversarial review.
5. Implement and verify changes selected by the user or coordinator under
   [change authority](../investigate/SKILL.md#change-authority). Use this cycle
   for behavioral repairs; for behavior-preserving refactoring, retain relevant
   tests green without manufacturing a new red. Complete the selected work or
   obtain an explicit disposition before final review.

## Separate design review

Routine refactoring during implementation remains implementer-owned. At the
cumulative feature completion boundary, arrange a separate design-review session
under host routing and authorization rules. Give the reviewer fresh context
through a verified harness mechanism; do not substitute the implementation
session's own assessment. If that mechanism is unavailable, report the stage
incomplete. This is one feature-level stage, not another review after every TDD
slice.

Resolve the installed [design-review](../design-review/SKILL.md) path and
instruct the reviewer to load it before investigating. Supply a concise brief
containing:

- The cumulative feature range, candidate `HEAD` and tree identity, any included
  uncommitted changes, readable checkout path, and the design-review assignment.
- The current `scope:` value and its source from `atd status` in the primary
  workspace; repository-local Git config may not be present in the review
  checkout.
- Intended behavior, representative consumers, constraints, relevant acceptance
  tests, and verification evidence with its candidate and coverage.
- If an active [feature contract](../feature-contract/SKILL.md) applies, give the
  design reviewer its primary-workspace path readable from that session. Present
  the contract as a claim to assess, not a definition of correctness.
- Applicable host instructions, execution limits, and relevant design decisions
  or retained tradeoffs as claims to assess, not conclusions to preserve.

Provide task context rather than the implementation transcript. Keep the
candidate stable during review; if it changes, establish which conclusions remain
applicable before presenting them as current. Resolve missing required context
or report the affected assessment incomplete.

Return proposals and material limits to the coordinator for disposition under
[change authority](../investigate/SKILL.md#change-authority). A proposal is not
authorization to edit.

Use the existing finding record when continuity is needed; distinguish proposal
IDs and decision-authority dispositions from adversarial findings. Follow its
content restrictions: keep decisions and references there, not source or API
sketches. Do not create another approval ledger or treat a record as authority.

Give the implementer the selected proposals, their dependencies and scope, and
behavior to preserve. Escalate material departures from that selection before
making them. Verify affected behavior and required host gates on the resulting
candidate; do not reuse pre-refactor test results as evidence for changed code.
Judge whether the selected design improvement was achieved and return remaining
tradeoffs and limits. Preserve any feature contract's readable path for the final
review handoff.

Do not recursively launch a full design review for each accepted refactor. If a
material new design question remains, surface it for the decision authority to
decide whether another bounded review is useful. Once selections are resolved,
hand the resulting candidate to [review](../review/SKILL.md) under its existing
authorization policy. Keep the final adversarial reviewer independent of the
design reviewer as well as the oracle author and implementer.

Follow project routing, commit, completion-gate and review/mutation authorization rules.
