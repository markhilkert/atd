# ATD Direction

Authoritative project direction. Keep below ~150 lines.

## What atd is

A cost-efficient adversarial TDD workflow targeting Codex, Claude Code, opencode,
and GitHub Copilot: high-quality, trustworthy code at the lowest practical total
cost. Quality takes priority over throughput. Test how much smaller models can
reliably perform, counting all coordination, verification, retries and
remediation. Adversarial review that finds real bugs and TDD discipline that
survives long contexts are load-bearing.

Design for capable, good-faith models. Prioritize test design, independent
investigation and execution evidence. Include inexpensive shortcut safeguards;
substantial effort or design compromises for them require evidence of benefit.

Build Codex and Claude Code support now as one Go binary with per-harness enable,
not plugins or symlinks. Develop the Claude Code integration in Claude Code and
validate it here and in a large established repository. Share workflow guidance,
review contracts and verification logic through thin installation, hook and agent
adapters. opencode and GitHub Copilot remain future targets. Defer a generalized
framework until concrete differences justify it. Installation must not copy this
repository's AGENTS.md.

## Instruction ownership

Each rule has one operational home; other documents link to it.

| Home | Owns |
|---|---|
| This file | Product direction, verifier semantics, boundaries and pilot purpose |
| [Skills](.agents/skills/investigate/SKILL.md) | Reusable investigation, spike, feature-contract, TDD and review-evaluation procedures; sole source of installed `atd-*` skills |
| [AGENTS.md](AGENTS.md) | Local conventions, workflow entry points, Codex routing and execution setup |
| [Review skill](.agents/skills/review/SKILL.md) | Review authorization, scope, launch, evidence checks, finding continuity and closure |
| [Reviewer skill](.agents/skills/reviewer/SKILL.md) | Independent investigation, experiments, finding fields and reviewer continuity |

## Three layers with different jobs

| Layer | Mechanism | Character |
|---|---|---|
| Steer | Harness hooks injecting short nudges | In-loop, cheap, advisory |
| Verify | ATD-owned test runner and exact-tree completion gates | Terminal, authoritative, local |
| Catch the rest | Adversarial review over a frozen range | Finds what tests are structurally blind to |

### Steer

- Use short, thresholded advisory pre-tool nudges. The hook is a dumb trigger
  without logic or payload parsing; `atd` answers from `git status`,
  `git diff --name-only` and one small state file. Keep shims near ten shell lines;
  adapt native event names, payloads and output. Both harnesses use `PreToolUse`.
- `atd enable claude|codex` writes hooks and embedded `atd-*` skills natively into
  one worktree, Git-excluded and never committed; `atd update` refreshes them from
  the running binary and refuses edited skills; `atd status` reports currency.
- Initialization registers user-owned `script/test_fast` and `script/test`;
  `atd test fast/full` runs exactly those scripts. `atd test -- <cmd>` records a
  targeted run. Compare the previous same-command observation to detect red→green
  and nudge on stdout. Hooks steer edits and runner use, but cannot reliably
  capture test status. The runner supplies that evidence and timely nudges.
- Never persist patch contents, prompt text or tool output.

### Verify

- Record mode, command, exit status, HEAD and exact tree in a local append-only Git ref.
- Inner cycles are observed, not certified. Report outer-red/binding evidence and
  bounded source/history-change, signal and launch-failure counts within the same
  worktree and relevant red starting heads; expand on demand. These are advisory,
  without cycle reconstruction, missing-inner-red inference or additional gates.
- Test-only outer-red commits are design/delegation artifacts. Missing, orphaned or
  suspect evidence is review input, not rejection grounds. No slice registry,
  intended-failure judgment, one-red-per-slice rule, portable trailers or per-cycle obligations.
- Completion requires stable green fast/full on the exact candidate tree;
  `script/test` is the repository's full-suite contract. Use the latest observation
  by ref append order matching starting tree, worktree, mode and registered command.
  A newer failed/unstable run invalidates older green. No targeted-green gate.
  When either gate is invalid, report both gate outcomes and recommend rerunning
  only invalid gates with the command in this worktree at the candidate's exact
  tree.
  Fresh clones rerun terminal gates, not local reds. Reject stale test/review evidence.

### Review

Follow the [review contract](.agents/skills/review/SKILL.md). Review complements
TDD: every material finding in two early independent reviews of atd was in code
that passed its test suite. ATD maintains no parallel test registry or approval
record.

## Planning: establish scope and readiness

Use the [investigation skill](.agents/skills/investigate/SKILL.md) before
implementation, [TDD](.agents/skills/tdd/SKILL.md) for implementation/remediation,
and [review evaluation](.agents/skills/evaluate-review/SKILL.md) on returned findings.
Native planning mode and its hooks remain deferred pending observed limitations.

Repository scope is an explicit developer choice: `owner-controlled` grants
broad refactoring discretion; `ticket-bounded` limits optional work by its added
verification burden. Required ticket work remains permitted even when testing is
substantial. The [investigation skill](.agents/skills/investigate/SKILL.md#change-authority)
owns the operational authority rule, including design-proposal selection. Store
the choice as repository-local `atd.scope`, shared by worktrees; unset repositories
gain no standing delegation. This is workflow guidance, not a mechanical gate or
a grant of review, push or deployment authority.

Keep the four investigation questions unless deliberately revised. For questions
1–3, gather evidence mechanically where cheaper, then judge risk coverage;
search hits are insufficient. Use deterministic, per-commit-cacheable `rg` and
cross-repo searches over existing checkouts. Infrastructure access, tickets, CI
logs and multi-repo checkout belong to other tools; `atd` owns workflow/exploration
without calling models. Durable codebase memory belongs behind future `atd explore`.
After the pilot decision, lift only recurring unchanged steps into that tool;
question 4 remains a skill. No callgraph or generalized multi-harness abstraction;
thin command wrappers are allowed. Build the loop first.

## Model routing: by oracle, not by task size

[AGENTS.md](AGENTS.md#model-routing) owns Codex routing. Claude Code routing is
unconfigured pending native development and validation; verify it natively before
relying on it. Preserve capable oracle judgment and independent review without
Codex model names.

## Out of scope

An earlier signed-brief workflow is retired. Do not restore, migrate, maintain
compatibility with, or reason from it. Delete signed briefs, bootstrap ledgers,
adoption/activation and session binding, canonical archives/symlinks, state.md
front matter, digest chains, reservation locks, terminal-authority revalidation,
schema migrations, compatibility paths and Codex `PostToolUse`.

Exclude mandatory spend/time instrumentation, continuous edit policing, session
narration, portable observation trailers, per-cycle provenance gates, range-wide
cycle reconstruction and copied diffs. No handoff files, plans or separate
specifications beyond a [feature contract](.agents/skills/feature-contract/SKILL.md)
when another session coordinates, or a [spike](.agents/skills/spike/SKILL.md) brief.
Named tests and concise commit contracts are the behavior record. The
[review contract](.agents/skills/review/SKILL.md#finding-record-and-continuity)
also permits human/agent-maintained host finding continuity outside ATD runner
state; developer-authorized host policies remain possible.
Enabled harnesses and developer-selected scope are recorded in shared Git config
`atd.harness` and `atd.scope`. Other runtime
state *owned by ATD* outside Git is one append-only JSONL file, folded on read, deletable
and rebuildable without locking. Lost, reordered or malformed cache events under
concurrency/crash are accepted; the observation ref remains CAS-protected. One
user and workspace means no migration problem.

## Known open questions

- Claude Code permissions and routing need evidence; opencode needs a hook probe.
- Routing savings and review transfer lack matched total-cost evidence.
- Hook latency is unmeasured; per-edit `git status` may be slow enough to disable.
- Durable codebase memory is unbuilt and may matter most for unfamiliar work repositories.

## Pilot protocol

Evaluate three to five real behaviors, including one in a large established repo
with unfamiliar consumers or deployment boundaries when access allows. Keep
exploration manual; attribute findings and overhead. Success on a small personal
project does not establish transfer to work with unknown callers or
underspecified requests. Do not cut exploration on that basis. Assess combined
usefulness, quality, rework, friction and total cost; the small sample cannot
isolate individual changes' causal effects or prove general effectiveness. At the
pilot decision, revisit deferrals and set policy here before expanding tooling.
The maintainer keeps pilot records outside this repository.
