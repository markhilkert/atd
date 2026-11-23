---
name: feature-contract
description: Write, or work from, the contract that hands a planned feature to a separate coordinating session. It states the feature's end state and decisions, not how to build it.
---

# Feature contract

Use a feature contract when one session plans a feature and another coordinates
its implementation. A feature is the unit of acceptance and one final review. The
contract states the feature's end state and the decisions behind it, not how to
build it. The developer owns its content; the planning session writes it with the
developer through [investigate](../investigate/SKILL.md) just before the feature
starts. The coordinator owns the rest of its [TDD](../tdd/SKILL.md) role,
including the acceptance tests. Routine implementation and refactor choices follow
[change authority](../investigate/SKILL.md#change-authority).

Keep it at `.atd/features/<id>.md` in the primary workspace. Give each contract or
spike brief a short identifier that is never reused; its review record and closed
record use the same identifier. Exclude `/.atd/features/` from Git the way the
[review contract](../review/SKILL.md#finding-record-and-continuity) excludes review
records. Give its path to sessions in other checkouts; ignored files do not travel.
With the handoff, include the current `scope:` value and source from primary-workspace
`atd status`; Git-local config may not clone. Recheck the primary workspace before
later consequential decisions so a changed setting takes effect.

## Contents

Use these sections in order and omit empty ones:

- **Header:** feature name, source (ticket, epic or design section) and the commit
  at which assumptions and starting points were checked.
- **Outcome:** what is true when the feature is done and for whom; consumers and the
  boundary they use; deployment limits; where this feature stops.
- **Behaviors:** one named example per behavior; its acceptance test may reuse the
  name. Give Given/When/Then with concrete values, observed where a consumer would
  observe it, and **Reject:** a plausible wrong result the test must catch. Write
  examples, not tests. Order does not prescribe slices.
- **Decisions:** each with a one-line reason. Include a requirement only if the
  developer would reject working code that did it differently: interfaces others
  depend on, data formats, error behavior, dependencies, rollout order, enforced
  structural rules. List items left to the coordinator only where a reader might
  otherwise stop to ask.
- **Boundaries:** what is not in this feature, and what other consumers rely on that
  must not change.
- **Open questions:** each with a default the coordinator can proceed on, or the
  behavior it blocks.
- **Assumptions:** what the contract relies on and how it was checked. If one proves
  false, stop and ask before building on it.
- **Starting points:** relevant code and non-obvious traps, checked at the header
  commit. They are pointers, not instructions.
- **Developer checks:** devices, accounts or visual judgment the coordinator cannot
  verify. Report them as pending rather than claiming them.

Never include step lists, pseudocode, internal signatures, test code, file-by-file
changes, session narration or review history.

A contract is ready when the coordinator can start without making a decision the
developer would want to make: every behavior has concrete values, an observation
point and a wrong result; every open question has a default or names the behavior
it blocks; assumptions and starting points were checked at the header commit; and
the developer would reject working code that broke any requirement in it.

When the feature cannot be specified until something is learned, run a
[spike](../spike/SKILL.md) first and write the contract from its results.

## Amendments

Before changing agreed acceptance criteria to add, change, or remove a binding
requirement or constraint, get the decision holder's express approval of the
exact current and proposed wording and the concrete solution and acceptance
behavior that would change. This applies whether criteria are recorded in a
feature contract, acceptance tests, or elsewhere. Preserve current written
criteria and tests until approval.
Existing delegation remains effective within its mandate. Approval to try an
approach authorizes that attempt only; it leaves alternatives open and does not
itself change the contract or acceptance criteria. Moving a settled question
into Decisions follows this procedure when it changes a binding requirement or
constraint. Routine technical choices and test changes within agreed behavior
need no routine approval. If implementation notes are recorded, mark them
replaceable and nonbinding for acceptance and review.

## Working from a contract

1. Before starting, check unresolved contract decisions under
   [change authority](../investigate/SKILL.md#change-authority); ask about
   developer-owned outcomes or boundaries. If HEAD has moved past the header
   commit, recheck the assumptions and starting points you rely on. Record the
   developer's answers in place.
2. Slice the feature. Write each acceptance test from its behavior's example, and
   brief implementers under TDD with whatever parts of the contract a slice needs.
   You may add tests within agreed behaviors; new behaviors and changes to
   decisions or boundaries go to the developer.
3. Keep the contract current. When a consequential decision changes, follow the
   [amendment procedure](#amendments) before updating the contract or briefing
   affected delegates; a message alone leaves stale written requirements. Move
   settled questions into Decisions with no change log in the active contract.
4. In the completion hand-back, add the decisions the contract left open that you
   made and anything in it that proved wrong.
5. Give the final reviewer the contract's readable path as claims to assess, not as
   the definition of correct.
6. After finding dispositions, record decisions later work depends on in a commit
   or the repository's docs, and move still-open questions to the finding record.
   Then close the contract: add a status line at the top with the closing date,
   outcome and evidence references, and move it to `.atd/features/closed/<id>.md`.
