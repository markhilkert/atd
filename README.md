# atd

Adversarial TDD for coding agents. atd grounds an agent's work in tests that
mean something, then has a separate agent try to break the result.

It works with Claude Code and Codex. It installs workflow skills and lightweight
hooks into your repository and adds a small CLI that records test runs and gates
completion on them.

## Why

Agents do their best work under test-driven development, but asking one to "use
TDD" rarely gets you TDD. What you usually get:

- Vapid tests that restate the implementation and can't fail for a real bug.
- Tests written after the code, so there was never a genuine red to go green.
- Unit tests only, with no outer test proving the behavior works where it is used.
- Discipline that fades as the session gets long.

Even good tests are blind to what nobody thought to assert. The most effective
way to find those bugs is an adversarial review: a reviewer that didn't write
the code and is actively trying to show that it's wrong. Agents rarely review
this way without a clear method and boundaries.

atd supplies both: a TDD procedure that works outside-in from a committed
failing test, and a framework for running and acting on independent reviews.

## How it works

atd acts at three points, from cheapest to most thorough.

**Nudges while the agent works.** Hooks fire before the agent edits a file or
runs a shell command. If source edits pile up without a test run, or the agent
runs tests in a way atd can't see, it gets a one-line reminder. Nudges are
advisory and never block the agent.

**Recorded tests and a completion gate.** The agent runs tests through
`atd test`, which records the command, exit status, commit and exact file tree
of each run in a local Git ref. atd uses that record to tell the agent when to
commit a failing test and when to commit the code that makes it pass. Before a
change is called done, `atd verify` requires passing fast and full suites
recorded on the exact tree being delivered. A pass on some earlier version of
the code doesn't count.

**Feature design proposals.** At feature completion, a separate reviewer
examines names, APIs, responsibility boundaries, and state transitions across
slices. It returns evidence-backed proposals and tradeoffs without editing the
candidate. The coordinator handles proposals and no-proposal results under
[change authority](.agents/skills/investigate/SKILL.md#change-authority). Selected
changes are verified before final review. This is guidance, not a mechanical
gate.

**Independent adversarial review.** When you ask for one, the agent freezes a
commit range and launches a reviewer with fresh context, separate from the
implementer and design reviewer. The reviewer reconstructs what the change should do,
traces callers and consumers, may run bounded experiments such as mutations in
an isolated checkout, and reports findings with evidence. The original agent
then judges each finding before fixing anything and watches for bugs that recur
in families.

The skills carry the procedure:

| Skill | Used by | Purpose |
|---|---|---|
| `atd-investigate` | Your agent | Before coding: find consumers and deployment boundaries, check whether the capability already exists, surface real ambiguity, and decide whether to implement, split or spike |
| `atd-spike` | Your agent | When uncertainty blocks a sound plan: write a bounded brief, run the investigation, and hand back evidence with its limits |
| `atd-feature-contract` | Your agent and a separate coordinating session | When another session will implement: record one feature's end state and decisions, not how to build it, then work from them |
| `atd-tdd` | Your agent and any implementer it delegates to | Write an acceptance test through the consumer boundary, commit it red, work inward in small red/green cycles, then coordinate the feature design stage |
| `atd-design-review` | A separate reviewer | Before final adversarial review, propose evidence-backed changes to a completed feature's names, APIs, and responsibility boundaries; do not edit |
| `atd-review` | Your agent | Launch and scope an independent adversarial review of a frozen range, only when you ask |
| `atd-reviewer` | The reviewer | Investigate independently, run bounded experiments, report findings with evidence and coverage limits |
| `atd-evaluate-review` | Your agent | Judge returned findings, spot recurring families, and decide what to fix now or defer |

## What a cycle looks like

The agent writes an acceptance test and runs it through atd. The output below
is real:

```console
$ atd test fast
--- FAIL: TestSlugLowercasesAndHyphenates (0.00s)
    slug_test.go:7: Slug("Hello World") = "Hello World", want "hello-world"
FAIL
atd: test is red; if this is the intended failure, commit the test before continuing
```

It commits the failing test, implements, and runs the tests again:

```console
$ atd test fast
ok  	example.com/slug	0.088s
atd: test is green after red; commit, then refactor with tests green before the next test
```

If it strays, a hook adds context to its next tool call:

```text
atd: source edits are accumulating without an observed test; run atd test fast
```

Before calling the work done, check the gates yourself or tell the agent to:

```console
$ atd verify --base <start> --candidate HEAD
verification gates passed
credible red observations: 1
bound red candidates: 1
...
candidate gates: fast green; full green
```

`atd verify` also reports whether a failing test was recorded before the
implementation. That evidence is input for review, not a gate.

## What review catches

A green suite can still be wrong. In one real change, a heartbeat had to keep
writing its timestamp even when a disk-space check failed. The test made the
check fail, let a second, healthy beat run, and only then confirmed the
timestamp was fresh. An independent review noticed that the failing beat could
skip its write without the test noticing, and a mutation confirmed it: the
second beat's write hid the loss. The repaired test runs one failing beat and
checks its timestamp immediately.

That's the question to keep asking of your own tests: could a later success
hide the failure this test is meant to catch? Tests check what someone thought
to assert; review looks for what they didn't.

## Quickstart

You need Go 1.24 or later, Git, and Claude Code (or Codex; see the
[reference](docs/reference.md#codex)).

Install:

```sh
go install github.com/markhilkert/atd@latest
atd version
```

Go installs into `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is
unset; that directory must be on your `PATH`.

Set up a repository:

```sh
cd your-repo
atd init             # creates stub script/test_fast and script/test
$EDITOR script/test_fast script/test
git add script && git commit -m "chore: register test scripts"
atd enable claude
```

Plain `atd init` leaves coordination scope unchanged. To set it explicitly, use
`atd init --scope owner-controlled` or `atd init --scope ticket-bounded`.
`atd status` shows the repository-local setting used by the workflow skills.

`script/test_fast` should run your quick suite and `script/test` the full suite
that decides whether a change is done. `atd enable` writes hooks to
`.claude/settings.local.json` and skills to `.claude/skills/atd-*`, and excludes
them from Git so nothing is committed.

Start a new Claude Code session and describe the change you want, prefixed
with `/atd-investigate`. Once the change is committed and green, ask for a
review with `/atd-review`.

## Updating

Replace the executable using the installation command above, keeping `GOBIN`
pointed at your existing installation directory. For unpublished local changes,
run `go install .` from the desired ATD checkout instead.

Then, in each worktree you want to refresh:

```sh
atd update
atd status
```

`atd update` refreshes hooks and embedded skills; it does not download a new
binary. It can reinstall repository-recorded harnesses in disabled worktrees.
Review new or changed Codex hooks with `/hooks`; `atd status` checks files,
not hook trust or delivery.

See [Updating](docs/reference.md#updating) for details.

## Commands

| Command | Purpose |
|---|---|
| `atd init [--scope owner-controlled\|ticket-bounded]` | Create stub test scripts and optionally set coordination scope |
| `atd enable claude\|codex` | Install hooks and skills in this worktree |
| `atd update` | Refresh hooks and skills from the installed binary |
| `atd status` | Show the configured scope and whether hooks and skills are installed and current |
| `atd disable claude\|codex` | Remove atd's hooks and skills from this worktree |
| `atd test fast\|full` | Run and record a registered suite |
| `atd test -- <command>` | Run and record a targeted command |
| `atd verify --base <commit> --candidate <commit>` | Check the completion gates for a candidate |
| `atd version [--check]` | Print the version, or check for a newer release |

The [reference](docs/reference.md) covers worktrees, updates, disabling, Codex
setup and what atd stores. [Internals](docs/internals.md) explains how each
mechanism works underneath, with every claim linked to the code.

## Status and limitations

atd is early. It has one regular user and has been used on a handful of real
changes. Expect the skills and output to change.

- Nudges are advisory. An agent can ignore them, and the gate only means
  something if you or your agent run `atd verify`.
- atd never calls a model. Review quality depends on the model you run the
  reviewer on, and each review costs a separate agent session.
- Test records stay in your clone. They are not pushed and there is no CI
  integration.
- The edit hook runs `git status`. Its latency on very large repositories has
  not been measured.
- Guidance on which models to route to which roles exists only for Codex so far.

## Developing atd

```sh
go build ./...
atd test fast    # go test -short
atd test full    # go test
```

atd is developed with atd. The workflow skills live in `.agents/skills/`, and
`atd enable` installs the copies embedded in the binary. [DIRECTION.md](DIRECTION.md)
sets the design boundaries and [AGENTS.md](AGENTS.md) holds the conventions for
agents working on atd itself.

## License

atd is licensed under the [Apache License 2.0](LICENSE).
