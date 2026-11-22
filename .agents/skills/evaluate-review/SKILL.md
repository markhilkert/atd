---
name: evaluate-review
description: Assess returned review findings before remediation, using prior findings to reconsider recurring issues. For coordinators; does not conduct or authorize independent review.
---

# Evaluate a review

Reject stale reviewed heads. Judge findings against code and agreed behavior,
using available history, watch areas, and verified behavior with its limits.
Treat reviewer remedies and recurrence links as proposals; invent no history
and exempt no area from scrutiny.

When a finding may be part of a broader pattern, inspect the related cases before
choosing the repair and tests. Testing one nearby example does not show that the
broader problem is covered. Recognize families by shared causes or violated
contracts, not files.
When a family recurs, consider whether a different mechanism would enforce the
contract more directly before extending the existing mechanism. After a second
accepted finding in the family, revisit the affected contract and design before
another narrow repair.
When a related defect survives a repair, identify the assumption shared by the
failures and examine where else the implementation relies on it. Explain whether
the repair changes that assumption or only handles the reported case.
Distinguish sharing a failure response from ensuring that every relevant failure
reaches it. When repairing a failure boundary, trace the complete operation,
including interpretation of returned data, and explain how the mechanism enforces
the required outcome. Add tests
where this examination reveals a plausible failure not already covered.
Judge the evidence for recurrence and plausible alternative mechanisms
independently. Record an explicit fix-now or defer rationale and next action for
each finding. Surface any rejected or narrowed oracle finding with both its
evidence and the coordinator's reason; unresolved disputes remain visible, while
disproven claims need not remain permanently disputed.
If reconsideration cannot settle the mechanism, offer a separately authorized
cold approach review of the fixation evidence, assumptions, root causes and
structurally different alternatives against the same contract and total repair
cost. Do not launch it automatically.
Continue the current approach unless an alternative meets the same contract
with justified total cost. Use proportionate methods, not a fixed checklist.
Recurrence alone does not require redesign or authorize broader scope. If a
repair materially changes a public promise or persisted-state model, revisit
scope and readiness with
[investigate](../investigate/SKILL.md) before implementation. Ask about genuine
behavior ambiguity; use [tdd](../tdd/SKILL.md) for authorized repairs, delegating
bounded execution under the project routing policy.

Keep coordinator dispositions distinct from reviewer hypotheses. Update the
project's finding record with reasons, next actions, repair/test references and
useful continuity. Follow the project's review policy; evaluation grants no
additional authorization. Keep reasoning proportionate, without a separate report,
fixed test count or extra review.
