---
name: review
description: Launch, scope and close an authorized independent adversarial review of a frozen change range. For coordinators; follow the developer-selected host review policy.
---

# Coordinate an adversarial review

The request is synthesized in-session; no prompt, packet or approval record is
maintained.

Follow the developer-selected host review policy. Without broader explicit
authorization, each review requires its own developer request; do not launch or
relaunch one automatically. Review authorization includes bounded diagnostic
tests and mutations in an isolated checkout of the frozen candidate unless the
developer narrows it. It does not authorize arbitrary repairs, live services,
deployment, pushing or expanded scope.

A developer may explicitly authorize up to N review rounds and specified
remediation for a named increment. Before using that authority, establish its
scope, repair authority, budget ceiling, stopping conditions and escalation
conditions. Stop after a complete round with no material findings or only
nonblocking findings, subject to their disposition. Escalate unresolved repeated
findings, ambiguous contracts, incomplete consequential investigation or material
scope expansion. If the budget runs out, report remaining work rather than
claiming success. A record of authorization is not required; the finding record
does not grant review or repair authority.

Use a cold reviewer separate from the oracle author, implementation specialist,
and design reviewer.
Follow the project's routing policy for reviewer selection and coordinator
judgment. Start the reviewer with fresh context and no implementation history,
using the active harness's verified mechanism. Integration judgment does not
substitute for an independent review.

## Finding record and continuity

Prefer the host's established finding record. If none fits, the coordinator
creates `.atd/reviews/<id>.md` in the primary workspace when review work needs a
record. Use the feature's or spike's identifier, stable across rounds and sessions;
store the base and each reviewed head in the record rather than using a changing
head as its identity. Inspect an existing file before editing it. Reuse the
assigned record without overwriting unrelated content or silently forking its
history.

Before writing fallback content, find the checkout's Git exclude file with
`git rev-parse --path-format=absolute --git-path info/exclude`. Idempotently add
`/.atd/reviews/` in a clearly user-owned block outside ATD-managed markers,
preserving other lines. Verify the intended path is effectively excluded with
`git check-ignore` and that Git does not already track it; exclusion cannot hide
a tracked file. If the path collides with tracked or unrelated content, choose a
valid existing host record or resolve a distinct untracked destination without
untracking or overwriting user files. Do not create fallback records on enable.

The coordinator maintains the record. Give reviewers the primary workspace's
readable record path or bounded excerpts explicitly; ignored files do not travel
with an isolated worktree or clone. Reviewers return findings and do not edit a
shared record. In fresh sessions, locate the matching behavior record through
host instructions or this convention. If history is unavailable, disclose the continuity limit without
inventing closure; missing local history does not veto a newly authorized review.

Keep completed records and their exclusion through disable and re-enable. Local
records are neither backups nor portable guarantees. Record content must not
copy source, patches, prompts, transcripts or secrets. This is editable review
continuity, not ATD runner state, a registry, an approval ledger or a review gate.

## Readiness and launch

Review a coherent feature or repair range with meaningful outer acceptance behavior
green and an implementer declaration of scope/readiness, after developer
authorization. A final feature review covers the whole feature; internal TDD
slices are not separate review gates unless separately authorized. A commit alone
is not a useful review unit. Freeze `base..head` to include the behavior map and
relevant oracle revisions. Derive behaviors from named acceptance tests,
executable contracts from assertions, and remaining assumptions from commit
descriptions. Set up an isolated checkout at the frozen head for experiments.

Resolve the installed [reviewer skill](../reviewer/SKILL.md) to a path readable
from that checkout. The fresh reviewer must load it before investigating; its fixed
method, experiment limits and result format live there. Resolve any required
host instructions and testing/review rules to readable paths or supply their
contents with source and version. Missing or unreadable required rules or skill
are setup problems to resolve or report as incomplete, not instructions to ignore.

Give the reviewer one concise request with only variable context:

- Authorized review kind, exact `base..head`, expected head SHA, and any explicit
  change-scope or experiment limits. For a final feature review, identify the
  authorized cumulative feature range. Include the current `scope:` value and its
  source as read from `atd status` in the primary workspace; repository-local Git
  config may not be present in the isolated checkout, so use the supplied value
  rather than its local status. Include the coordinator's
  concise design conclusion and proposal dispositions, who selected each and
  under what scope or explicit delegation, completed changes, material retained
  tradeoffs or limits, and any [feature contract](../feature-contract/SKILL.md) by
  readable path as claims for independent assessment. A decision to retain a
  design is not an exemption from reporting a supported concern. The reviewer may
  inspect broader callers, consumers and history only to understand effects of
  that scope.
- Isolated checkout path at the frozen head and resolved installed reviewer skill
  path, with an instruction to load that skill before review.
- Behavior map, contracts, known assumptions, actual acceptance and gate evidence
  bound to this candidate, and compact outer-red/binding evidence and anomalies.
  Never claim gates passed without head-bound evidence.
- In **every** brief, initial or follow-up: applicable host-project instructions,
  testing and review rules, required wrappers, permissions and execution limits;
  use readable references or contents with source and version.
- Finding-record location and selected prior findings, watch areas, and verified
  behavior with limits, unless the developer explicitly requests a blind comparison.
- Any narrower experiment authorization and the resolved
  [evaluation skill](../evaluate-review/SKILL.md) path for the coordinator reminder.

Omit duplicated diffs and session narration. Shorten follow-up briefs with source
references and compact continuity, while retaining every required input above.
Only the developer may narrow change scope; state the resulting coverage limit.
Cold means independent of implementation, not necessarily blind to findings.
For a blind comparison, omit prior findings and continuity; the coordinator still
compares results with history. Never invent positive claims from prior reviews.

### Standalone spike review

A review of a [spike](../spike/SKILL.md) may be authorized at the spike's own
boundary; it does not replace final feature review. The spike is ready when its
hand-back is complete. Freeze the identified artifacts and evidence by version or
digest, and use those identities wherever this contract names `base..head`, the
head SHA or the candidate tree. Experiment on an isolated copy and preserve the
frozen originals. Supply test and gate evidence only where the spike's method
produced it; never claim production gates for an experiment. Give the reviewer
the brief and hand-back by readable path with the other required inputs.

## Return and disposition

Use [evaluate-review](../evaluate-review/SKILL.md) when the result returns and
before remediation; give the reviewer its resolved path for the reminder.
Immediately before accepting results, compare the reported reviewed head SHA
with current `HEAD`. Reject a stale result. Verify the candidate tree is
unchanged and experimental edits have been restored; matching commit SHAs alone
do not establish either condition.

Keep one row per finding in the selected finding record: stable ID, violated
contract/scenario, coordinator disposition and reason, next action, repair commit,
named regression evidence and useful related-finding links. Keep reviewer
recommendations and recurrence hypotheses distinct from coordinator decisions.
Update the latest compact continuity rather than accumulating transcripts. Retain
the reviewed head, range, requested versus confirmed routing with its evidence or
unknowns, and reviewer-skill version/hash and supplied rule/history revisions
when available; paths alone do not establish a revision. Optional token totals
must say what they measure. Reviewer recommendations do not authorize repairs.

A remediation delegate receives the selected finding and bounded relevant context,
then inspects code and runs targeted verification; it need not inherit the full
implementation session. Before completing a behavior, inspect open findings.
Accepted fixes need closure evidence or explicit deferral with rationale and a
next action. Revisit deferrals at the agreed decision boundary. This record
authorizes no additional review or remediation.
