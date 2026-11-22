---
name: design-review
description: Review a completed ATD feature's names, APIs, and boundaries before final adversarial review. Return proposals to the coordinator; do not edit.
---

# Review a feature's design

Act as the design reviewer. Return proposals to the coordinator. Do not edit the
candidate, write tests into it, or implement a proposal. This review does not
authorize remediation or launch the final adversarial review. Follow supplied
host instructions and execution limits. If a fresh reviewer session cannot be
established, report the stage incomplete; do not substitute a same-session
self-assessment.

## Establish the design context

Confirm the cumulative feature range and candidate identity. Read the affected
code, relevant tests, representative production callers, and nearby abstractions
and conventions. Trace enough surrounding code to understand the feature across
implementation slices; the diff alone is not the design.

Establish intended behavior, domain vocabulary, state and effect ownership, and
constraints on changing interfaces. Account for framework callbacks,
transactions, persistence, compatibility, and rollout where relevant. Treat
implementer explanations as claims to examine. Passing tests establish neither
a useful abstraction nor a good public API.

Concentrate on design introduced, extended, or newly relied upon by this feature.
Include existing code only when a bounded change there would materially improve
the feature. Do not turn the review into general legacy cleanup. Use available
context before asking questions. If missing evidence blocks a consequential
judgment, identify the affected assessment and what would resolve it; continue
independent parts of the review.

For stateful concerns, trace relevant state transitions and the invariants each
must preserve across sibling paths. Follow how each path changes state, handles
failure, and communicates with other owners. Judge whether a proposed boundary
reduces coordination across those paths; relocating tangled state alone is not
an improvement.

## Judge the design

Consider these questions together, using the code and its consumers rather than
a scorecard.

**Names.** Do names convey the domain concept and operation's intent? Are units,
results, state transitions, or effects unclear where a reader needs them? Check
misleading names as well as vague ones. Propose exact replacements grounded in
local vocabulary. Prefer an idiomatic, sufficient name to one that repeats
information visible in its type or context. If domain meaning is uncertain, say
so rather than inventing terminology.

**Public APIs.** What must a caller know, supply, or do in what order? Can
callers use the capability through a small, coherent contract, or must they
manage internal state, interpret flags, sequence setup and reads, or reconstruct
invariants? Examine actual uses and failure behavior. Judge the knowledge it
demands and guarantees it provides, not just its method count. A class with one
generic entry point can still expose a poor contract.

**Responsibilities and boundaries.** Where does a coherent policy, calculation,
transformation, or coordination responsibility live? Look for related logic
scattered among helpers or objects, unrelated reasons for change, and tests
that need broad setup to exercise a narrow concept. Consider whether a service,
policy, value object, module, function, or existing domain abstraction would give
that concept a useful home.

Before recommending extraction, describe what the proposed boundary owns, its
inputs, outputs and effects, and what responsibility leaves the original owner.
Check how callers and tests would use it. Moving private methods into another
object while preserving the same tangled dependencies is not enough. A single
caller does not rule out a useful boundary; many private methods or a large
class do not establish one. Keep cohesive implementation details private when
extraction would mostly add navigation or forwarding. Do not make methods public
merely to test them, or require every policy to become a service class.

For an improvement, compare it with retaining the design and with a simpler
local change when credible. Weigh clearer meaning, stronger invariants, and
easier independent reasoning or testing against new concepts, coupling,
navigation, interface surface, migration cost, and behavioral risk. Preserve
useful framework conventions; explain any justified departure. Do not presume
either conformity or extraction is always cheaper.

Recommend the smallest coherent change that resolves the identified burden;
the fewest changed lines are not necessarily the simplest design. Ground the
benefit in a concrete reading, usage, testing, or change scenario. An observed
bug or repeated historical incident is not required. Label hypothetical future
changes as such; do not justify speculative generality with invented needs.

## Diagnostics

The review is read-only by default. Run a diagnostic only when the brief or the
user explicitly authorizes that bounded experiment and states applicable limits.
Use a disposable isolated copy of the candidate; do not experiment in the primary
checkout or modify the candidate. Record the copy's starting identity and keep
independent experiments separate.

After each experiment, discard its disposable copy before starting another, or
restore the copy fully and verify its head, tree, and tracked, untracked, and
ignored files match its starting state. If full restoration cannot be confirmed,
stop experiments and report the review incomplete with the restoration limit.
An experiment does not authorize repairs or changes to the candidate.

## Return a decision-ready result

Report the reviewed candidate and range, code and caller paths examined, material
context limits, and any checks actually executed. Distinguish inspection from
execution. Mark the assessment incomplete if a consequential scoped design
question could not be assessed; missing evidence is not a clean result.

Give one prioritized batch of worthwhile proposals. Group changes that need to
be considered together and make independent choices separately selectable. Use
stable proposal IDs for follow-up. For each proposal include, in proportion to
its size:

- **Evidence and burden:** locations and a concrete example of what is difficult,
  misleading, or unnecessarily exposed today.
- **Proposed change:** exact names or a compact before/after API and caller sketch.
  For a boundary change, identify ownership, dependencies, effects, and what
  remains with the original owner. Sketch only enough to make the decision
  concrete; do not implement a patch.
- **Why this option:** benefit, credible cheaper alternative (including
  retaining the design), added cost or downside, uncertainty, and evidence that
  could change the recommendation.
- **Scope and verification:** affected callers and compatibility constraints,
  behavior to preserve, relevant tests, and meaningful coverage needed.
  Distinguish behavior-preserving internal API changes from changes to externally
  supported behavior or compatibility promises.

For an owner-controlled or ticket-bounded host scope, label each proposal as
**in-scope**, **scope extension**, or **separate follow-up**. Do not suppress
out-of-scope concerns or treat a recommendation as authorization to edit. Use
the scope value and source in the brief; do not infer it from the review checkout.

Do not require an extraction, finding count, numerical quality score, or a
proposal in every category. “No worthwhile change identified” is valid. When
useful, briefly explain a tempting change you rejected; do not manufacture a
rejected proposal to fill the report. Exclude pure preference and cosmetic churn
without a concrete readability benefit.

Surface incidental correctness or security concerns separately for the existing
finding process; do not bury them in optional design proposals or expand into
another adversarial investigation. Return proposals, including a no-proposal
result and material limits, to the coordinator. Disposition follows
[change authority](../investigate/SKILL.md#change-authority); recommendations
are not authorization to edit.
