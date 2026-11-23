# atd reference

Details behind the [README](../README.md): what each command writes, how the
gates decide, and how atd behaves across worktrees. For how these mechanisms are
implemented, see [internals](internals.md).

## Test scripts

`atd init` creates `script/test_fast` and `script/test` if they don't exist.
The stubs fail with a message until you replace them. Commit them; they are part
of your repository.

`atd init --scope owner-controlled` or `atd init --scope ticket-bounded` also
sets the repository's coordination scope. Plain `atd init` leaves that setting
unchanged. Scope is stored in local Git config shared by linked worktrees, and
`atd status` reports it on one `scope:` line. Only one of the two recognized
values is accepted; invalid or duplicate values are reported with a repair
command. The workflow skills read current status when used; see
[change authority](../.agents/skills/investigate/SKILL.md#change-authority) for
how the values guide coordinator decisions.

- `script/test_fast` is the quick suite the agent runs during red/green cycles.
- `script/test` is the full suite. Passing it is what "done" means, so it should
  run everything your team would expect to pass before merging. If part of your
  suite can't run locally, the full gate doesn't cover that part.

`atd test fast` and `atd test full` run exactly those scripts.
`atd test -- <command>` runs and records any other command, such as a single
test.

## Enabling a harness

```sh
atd enable claude   # or: atd enable codex
```

`atd enable` writes only into the current worktree and never commits:

| Harness | Hooks | Skills |
|---|---|---|
| Claude Code | `.claude/settings.local.json` | `.claude/skills/atd-*/SKILL.md` |
| Codex | `.codex/hooks.json` | `.agents/skills/atd-*/SKILL.md` |

It adds matching lines to the repository's `.git/info/exclude` under a
`# atd <harness>` comment, so the files never show up as changes. Existing hooks
and settings are preserved, and atd only touches the handlers it marks as its
own. It refuses to write a hooks file or skill that Git tracks, because
excluding a tracked file doesn't hide changes to it.

The enabled harness is recorded in the repository's Git config
(`git config --local --get-all atd.harness`), which all worktrees share.

Hooks call the absolute path of the `atd` binary that ran `enable`. If you
reinstall atd somewhere else, run `atd update` so the hooks point to it.

After enabling, start a new Claude Code session to load the hooks and skills.

### Codex

Codex requires you to trust new or changed hooks. After `atd enable codex` or
an `atd update` that changes hooks, run `/hooks` in Codex to review and trust
them. `atd status` checks the files atd wrote, not whether Codex trusts them.

## Updating

```sh
atd version --check   # is a newer release available?
go install github.com/markhilkert/atd@latest
atd update            # rewrite hooks and skills from the new binary
atd status            # confirm everything is current
```

`atd version --check` reads release tags with `git ls-remote`, so it needs the
same GitHub access as installing. It exits nonzero when a newer release exists,
when the build version can't be compared, or when the check fails. A development
build newer than the latest release passes.

`atd update` covers every harness recorded for the repository and removes
skills that newer releases dropped. It refuses to write anything if an `atd-*`
skill was edited by hand or wasn't written by atd; move those skills aside and
run it again.

## Disabling

```sh
atd disable claude           # or: atd disable codex
atd disable --force claude   # also remove edited or foreign atd-* skills
```

`atd disable` removes atd's hook handlers and `atd-*` skills from the current
worktree and keeps every other setting. A hooks file left empty is deleted. It
refuses to write anything when an `atd-*` skill was edited or wasn't written by
atd (unless `--force`), or when Git tracks a file it would change.

The exclude lines and the `atd.harness` record are shared by all worktrees.
`atd disable` removes them only when no other worktree still has that harness
installed; otherwise it names the worktrees that do. While the record remains,
`atd status` suggests re-enabling the worktree and `atd update` reinstalls it.
To turn a harness off completely, disable it in every worktree.

## Worktrees

Hooks and skills are installed per worktree. After `git worktree add`, run
`atd enable <harness>` there. If the repository already records a harness,
`atd update` can install its files instead, together with all other recorded
harnesses. A new worktree has no atd files to tell an agent that, so
add this to your user-level agent instructions (for example `~/.claude/CLAUDE.md`
or `~/.codex/AGENTS.md`):

```markdown
If `git config --local --get-all atd.harness` prints anything and `atd status` says this
worktree is not enabled, run the `atd enable <harness>` command it suggests.
```

Claude Code also loads the main checkout's `.claude/settings.local.json` hooks
and `.claude/skills` in linked worktrees. Identical atd hooks are deduplicated
and a worktree's own skills take precedence. However, if the main checkout's
hooks point to an old atd path, the linked worktree gets a second hook. Run
`atd update` in both places to prevent this.

## Nudges

Hooks run before the agent edits a file or runs a shell command. They check
`git status` and atd's local state, never the content of the edit or command.

| Situation | Nudge |
|---|---|
| Source edits pile up without a recorded test run | `source edits are accumulating without an observed test; run atd test fast` |
| A test file changed without a recorded run | `run the changed test with atd test fast or atd test -- <command> and confirm the intended red` |
| Several shell commands run while code has changed and no test was recorded | `if this command runs tests, use atd test fast, full, or -- <command> so the result is observed` |

Each nudge fires once and stays quiet until the next recorded test run.

`atd test` also reports on the result. A failing fast or targeted run says to
commit the test if that failure is the intended one. A run that passes after the
same command failed says to commit the implementation. If files or `HEAD` change
while the tests run, `atd test` fails and says so.

## Verification

```sh
atd verify --base <commit> --candidate <commit>
```

`atd verify` passes only when the candidate has a stable, passing
`atd test fast` run and a stable, passing `atd test full` run recorded on its
exact file tree. A run is stable if neither the files nor `HEAD` changed while
it ran. Runs made before committing count, as long as the files were identical.
For each gate, the latest matching run decides: a newer failing or unstable run
cancels an older pass. Targeted runs never satisfy a gate.

If either gate is invalid, `atd verify` reports both gate results: `passed`,
`no matching record`, `failed` with its exit, signal or launch reason, or
`changed files` and/or `changed history`. It recommends rerunning only invalid
gates with the command in this worktree at the candidate's exact tree, such as
`atd test fast`, and exits with status 1.

It also summarizes evidence for review, without affecting the result:

- **Credible red observations:** stable failing fast or targeted runs.
- **Bound red candidates:** failing runs recorded at commits between base and
  candidate, typically the committed acceptance test before its implementation.
- **Run anomalies:** runs where files or history changed mid-run, the test was
  killed by a signal, or the command failed to launch.

Missing red evidence is something for a reviewer to ask about, not a reason to
reject the change.

Test records are local. A fresh clone has none, so run the fast and full suites
there before verifying.

## What atd stores

- `refs/atd/test-observations`: an append-only Git ref with one record per
  `atd test` run: the mode, command, exit status, `HEAD`, file tree before and
  after, worktree path and time. It is never pushed unless you push it
  explicitly.
- `.git/atd/test-observation-index.jsonl`: a cache of those records plus nudge
  state. It can be deleted safely and is rebuilt when needed.
- `atd.harness` in the repository's Git config.
- `atd.scope` in repository-local Git config, shared by linked worktrees. Status
  ignores global config, reports `unconfigured` when absent, and reports
  `invalid` for unknown or multiple values; explicit `atd init --scope ...`
  repairs it.

atd never stores patch contents, prompts or tool output.
