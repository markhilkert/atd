---
name: spike
description: Run a bounded investigation when uncertainty prevents a sound implementation plan, then hand back evidence with its limits. Covers the brief, conduct, hand-back and closure.
---

# Spike

Use a spike when something must be learned before dependent work can be planned
soundly, or when implementation disproves an assumption the plan relies on. A spike
normally comes before the dependent implementation plan; it can also be the whole
piece of work. The developer (or whoever they have delegated it to) commissions the
spike and decides what its results justify.

## Brief

Before starting, write a brief at `.atd/features/<id>.md`, following the
[feature contract](../feature-contract/SKILL.md) rules for location and
identifiers: the question, a budget, method limits and the evidence to return.
Give the spike its own identifier, even when a feature follows it.

## Conduct

Use inspection or a focused experiment within existing authorization. Do not
implement a miniature feature or investigate every edge case. Stop when the
question is answered or the budget is reached, then reassess.

## Hand-back

Identify the artifact and its version, the observation method and the controls.
Keep observed results separate from hypotheses and setup failures, and state the
limits and the questions still open. Do not infer success from an absent effect
unless a control showed the method could detect it: a missing response, early
termination or a completed runner does not by itself establish the property under
test. A spike result does not establish production acceptance.

## Closure and review

Close the brief the way a feature contract is closed: add a status line with the
closing date, outcome and evidence references, and move it to
`.atd/features/closed/<id>.md`. When an independent review of the spike is
authorized, follow the [review contract](../review/SKILL.md#standalone-spike-review).
