package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

type harnessForTest struct {
	name           string
	hookSubcommand string
	hooksPath      string
	skillsRoot     string
	editTools      []string
	unrelatedTools []string
	matcherSelects func(*testing.T, string, string) bool
}

var harnessesForTest = []harnessForTest{
	{
		name:           "claude",
		hookSubcommand: "_claude-hook",
		hooksPath:      ".claude/settings.local.json",
		skillsRoot:     ".claude/skills",
		editTools:      []string{"Edit", "Write", "NotebookEdit"},
		unrelatedTools: []string{"Read", "Task", "Agent", "mcp__filesystem__write_file"},
		matcherSelects: claudeMatcherSelectsForTest,
	},
	{
		name:           "codex",
		hookSubcommand: "_codex-hook",
		hooksPath:      ".codex/hooks.json",
		skillsRoot:     ".agents/skills",
		editTools:      []string{"apply_patch", "Edit", "Write"},
		unrelatedTools: []string{"mcp__filesystem__read_file", "update_plan", "Agent"},
		matcherSelects: regexpMatcherSelectsForTest,
	},
}

var sourceSkillNamesForTest = []string{"design-review", "developer-proxy", "evaluate-review", "feature-contract", "investigate", "review", "reviewer", "spike", "tdd"}

var sourceSkillsDirectoryForTest = func() string {
	directory, err := filepath.Abs(".agents/skills")
	if err != nil {
		panic(err)
	}
	return directory
}()

func TestEnableInstallsWorkingHooksWithoutReplacingExistingConfiguration(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, test := range []struct {
			name     string
			existing []byte
		}{
			{name: "pristine repository"},
			{name: "existing hooks", existing: existingHookConfigurationForTest(harness)},
		} {
			t.Run(harness.name+"/"+test.name, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				hooksPath := filepath.Join(repository, harness.hooksPath)
				var before map[string]any
				wantMode := os.FileMode(0o600)
				if test.existing != nil {
					writeFileForTest(t, hooksPath, test.existing)
					wantMode = 0o640
					if err := os.Chmod(hooksPath, wantMode); err != nil {
						t.Fatal(err)
					}
					before = decodeJSONForTest(t, hooksPath, test.existing)
				}
				marker := installFakeATDForTest(t)

				withTestRunnerDirectory(t, repository)
				var output bytes.Buffer
				if err := runCLI([]string{"enable", harness.name}, &output); err != nil {
					t.Fatal(err)
				}
				if harness.name == "codex" && !strings.Contains(output.String(), "/hooks") {
					t.Fatalf("enable output does not explain Codex hook trust: %q", output.String())
				}
				if _, err := os.Stat(filepath.Join(repository, "script")); !os.IsNotExist(err) {
					t.Fatalf("enable also initialized test scripts: %v", err)
				}

				configuration := readJSONForTest(t, hooksPath)
				assertExistingHookConfigurationForTest(t, before, configuration)
				info, err := os.Stat(hooksPath)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != wantMode {
					t.Fatalf("hooks file mode = %o, want %o", info.Mode().Perm(), wantMode)
				}
				if contents, _ := os.ReadFile(hooksPath); test.existing != nil && !bytes.Contains(contents, []byte("make test && make lint")) {
					t.Fatalf("hand-written configuration text was re-escaped:\n%s", contents)
				}
				assertInstalledHooksForTest(t, harness, repository, before, configuration, marker)

				snapshot := snapshotRepositoryForTest(t, repository)
				output.Reset()
				if err := runCLI([]string{"enable", harness.name}, &output); err != nil {
					t.Fatalf("second enable: %v", err)
				}
				if !strings.Contains(output.String(), "atd update") {
					t.Fatalf("second enable does not point to atd update: %q", output.String())
				}
				assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
			})
		}
	}
}

func TestEnableInstallsSkillsInLinkedWorktreeOnly(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			hostSkill := filepath.Join(repository, harness.skillsRoot, "host-skill", "SKILL.md")
			hostContents := []byte("---\nname: host-skill\ndescription: Host-owned skill.\n---\n\nHost body.\n")
			writeFileForTest(t, hostSkill, hostContents)
			runTestRunnerGit(t, repository, "add", "--", harness.skillsRoot)
			runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: add host skill")
			worktree := newLinkedWorktreeForTest(t, repository)
			mainSnapshot := snapshotWorkingFilesForTest(t, repository)
			installFakeATDForTest(t)

			withTestRunnerDirectory(t, worktree)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}

			assertInstalledSkillsForTest(t, harness, worktree, embeddedSourceSkillsForTest(t))
			assertFileContentsForTest(t, filepath.Join(worktree, harness.skillsRoot, "host-skill", "SKILL.md"), hostContents)
			if status := runTestRunnerGit(t, worktree, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("enable left files visible to Git in the worktree:\n%s", status)
			}
			if harnesses := runTestRunnerGit(t, repository, "config", "--get-all", "atd.harness"); harnesses != harness.name+"\n" {
				t.Fatalf("repository harness record = %q, want %q", harnesses, harness.name)
			}
			assertSnapshotUnchangedForTest(t, mainSnapshot, snapshotWorkingFilesForTest(t, repository))
			for _, path := range []string{harness.hooksPath, filepath.Join(harness.skillsRoot, "atd-tdd", "SKILL.md")} {
				runTestRunnerGit(t, repository, "check-ignore", "-q", "--no-index", "--", path)
			}
			if exec.Command("git", "-C", repository, "check-ignore", "-q", "--no-index", "--", filepath.Join(harness.skillsRoot, "host-skill", "SKILL.md")).Run() == nil {
				t.Fatal("enable excluded a host-owned skill")
			}
		})
	}
}

func TestEnableLeavesMalformedConfigurationUnchanged(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			hooksPath := filepath.Join(repository, harness.hooksPath)
			writeFileForTest(t, hooksPath, []byte("{\"hooks\":"))
			snapshot := snapshotRepositoryForTest(t, repository)

			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err == nil {
				t.Fatal("enable accepted malformed existing hooks")
			}
			assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
		})
	}
}

func TestEnableRejectsUnsafeConfigurationPaths(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, test := range []struct {
			name string
			link func(harnessForTest) (string, bool)
		}{
			{name: "configuration directory symlink", link: func(h harnessForTest) (string, bool) { return filepath.Dir(h.hooksPath), true }},
			{name: "hooks file symlink", link: func(h harnessForTest) (string, bool) { return h.hooksPath, false }},
			{name: "skills root symlink", link: func(h harnessForTest) (string, bool) { return h.skillsRoot, true }},
		} {
			t.Run(harness.name+"/"+test.name, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				outside := t.TempDir()
				writeFileForTest(t, filepath.Join(outside, "outside.txt"), []byte("outside\n"))
				linkPath, directory := test.link(harness)
				target := outside
				if !directory {
					target = filepath.Join(outside, "outside.txt")
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Join(repository, linkPath)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(repository, linkPath)); err != nil {
					t.Fatal(err)
				}
				outsideBefore := snapshotWorkingFilesForTest(t, outside)
				installFakeATDForTest(t)

				withTestRunnerDirectory(t, repository)
				if err := runCLI([]string{"enable", harness.name}, io.Discard); err == nil {
					t.Fatal("enable accepted unsafe configuration path")
				}
				assertSnapshotUnchangedForTest(t, outsideBefore, snapshotWorkingFilesForTest(t, outside))
			})
		}
	}
}

func TestUpdateReconcilesOwnedHooks(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			hooksPath := filepath.Join(repository, harness.hooksPath)
			configuration := readJSONForTest(t, hooksPath)
			preToolUse := arrayForTest(t, objectForTest(t, configuration["hooks"], "hooks")["PreToolUse"], "PreToolUse")
			firstGroup := objectForTest(t, preToolUse[0], "first ATD group")
			firstGroup["user_metadata"] = map[string]any{"preserve": true}
			firstGroup["hooks"] = append(arrayForTest(t, firstGroup["hooks"], "first ATD handlers"), map[string]any{
				"type": "command", "command": "user-added-handler", "timeout": json.Number("23"),
			})
			encoded, err := json.MarshalIndent(configuration, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			writeFileForTest(t, hooksPath, append(encoded, '\n'))

			marker := installFakeATDForTest(t)
			if err := runCLI([]string{"update"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			after := readJSONForTest(t, hooksPath)
			userAdded, metadata := 0, false
			for _, groupValue := range arrayForTest(t, objectForTest(t, after["hooks"], "hooks")["PreToolUse"], "PreToolUse") {
				group := objectForTest(t, groupValue, "PreToolUse group")
				if value, ok := group["user_metadata"].(map[string]any); ok && value["preserve"] == true {
					metadata = true
				}
				for _, handlerValue := range arrayForTest(t, group["hooks"], "PreToolUse handlers") {
					handler := objectForTest(t, handlerValue, "PreToolUse handler")
					if handler["command"] == "user-added-handler" && handler["timeout"] == json.Number("23") {
						userAdded++
					}
				}
			}
			if userAdded != 1 || !metadata {
				t.Fatalf("reconciled hooks lost user additions: user_added=%d metadata=%t: %#v", userAdded, metadata, after)
			}
			before := map[string]any{
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"matcher":       firstGroup["matcher"],
							"user_metadata": firstGroup["user_metadata"],
							"hooks": []any{
								map[string]any{"type": "command", "command": "user-added-handler", "timeout": json.Number("23")},
							},
						},
					},
				},
			}
			assertInstalledHooksForTest(t, harness, repository, before, after, marker)
		})
	}
}

func TestUpdateRefreshesStaleFilesAndPrunesRetiredSkills(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			useEmbeddedSkillsForTest(t, previousReleaseSkillsForTest())
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertInstalledSkillsForTest(t, harness, repository, previousReleaseSkillsForTest())

			current := embeddedSourceSkillsForTest(t)
			useEmbeddedSkillsForTest(t, current)
			marker := installFakeATDForTest(t)
			want := harness.name + ": repository enabled; worktree enabled; hooks stale; skills stale atd-evaluate-review, atd-investigate, atd-tdd; skills missing atd-design-review, atd-developer-proxy, atd-feature-contract, atd-review, atd-reviewer, atd-spike; skills retired atd-retired; run atd update"
			if line := statusLineForTest(t, harness); line != want {
				t.Fatalf("status before update:\n got %q\nwant %q", line, want)
			}

			if err := runCLI([]string{"update"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertInstalledSkillsForTest(t, harness, repository, current)
			assertInstalledHooksForTest(t, harness, repository, nil, readJSONForTest(t, filepath.Join(repository, harness.hooksPath)), marker)
			if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("update left files visible to Git:\n%s", status)
			}
			if line := statusLineForTest(t, harness); line != harness.name+": repository enabled; worktree enabled; hooks current; skills current" {
				t.Fatalf("status after update = %q", line)
			}
		})
	}
}

func TestUpdateRequestsCodexTrustWhenHookCommandChanges(t *testing.T) {
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"enable", "codex"}, io.Discard); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runCLI([]string{"update"}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "/hooks") {
		t.Fatalf("unchanged Codex hooks prompted for trust: %q", output.String())
	}

	installFakeATDForTest(t)
	output.Reset()
	if err := runCLI([]string{"update"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "/hooks") {
		t.Fatalf("changed Codex hooks did not prompt for trust: %q", output.String())
	}
}

func TestUpdateRefusesHandEditedSkillsBeforeWritingAnything(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			useEmbeddedSkillsForTest(t, previousReleaseSkillsForTest())
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			edited := filepath.Join(harness.skillsRoot, "atd-investigate", "SKILL.md")
			contents, err := os.ReadFile(filepath.Join(repository, edited))
			if err != nil {
				t.Fatal(err)
			}
			writeFileForTest(t, filepath.Join(repository, edited), bytes.Replace(contents, []byte("Investigate"), []byte("Locally investigate"), 1))
			foreign := filepath.Join(harness.skillsRoot, "atd-host", "SKILL.md")
			writeFileForTest(t, filepath.Join(repository, foreign), []byte("---\nname: atd-host\ndescription: Host skill with an atd prefix.\n---\n"))

			useEmbeddedSkillsForTest(t, embeddedSourceSkillsForTest(t))
			installFakeATDForTest(t)
			want := harness.name + ": repository enabled; worktree enabled; hooks stale; skills stale atd-evaluate-review, atd-tdd; skills missing atd-design-review, atd-developer-proxy, atd-feature-contract, atd-review, atd-reviewer, atd-spike; skills retired atd-retired; skills edited atd-host, atd-investigate; run atd update"
			if line := statusLineForTest(t, harness); line != want {
				t.Fatalf("status with edited skills:\n got %q\nwant %q", line, want)
			}
			snapshot := snapshotRepositoryForTest(t, repository)
			err = runCLI([]string{"update"}, io.Discard)
			if err == nil {
				t.Fatal("update overwrote hand-edited skills")
			}
			for _, path := range []string{edited, foreign} {
				if !strings.Contains(err.Error(), path) {
					t.Fatalf("update error %q does not name %s", err, path)
				}
			}
			assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
		})
	}
}

func TestUpdateChecksEveryHarnessBeforeWritingAny(t *testing.T) {
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	useEmbeddedSkillsForTest(t, previousReleaseSkillsForTest())
	withTestRunnerDirectory(t, repository)
	for _, harness := range harnessesForTest {
		if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	last := harnessesForTest[len(harnessesForTest)-1]
	edited := filepath.Join(last.skillsRoot, "atd-tdd", "SKILL.md")
	writeFileForTest(t, filepath.Join(repository, edited), []byte("---\nname: atd-tdd\ndescription: Locally rewritten.\n---\n"))
	useEmbeddedSkillsForTest(t, embeddedSourceSkillsForTest(t))
	installFakeATDForTest(t)
	snapshot := snapshotRepositoryForTest(t, repository)

	err := runCLI([]string{"update"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), edited) {
		t.Fatalf("update error = %v, want refusal naming %s", err, edited)
	}
	assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
}

func TestEnableRefusesExistingForeignSkillBeforeWritingAnything(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			foreign := filepath.Join(harness.skillsRoot, "atd-tdd", "SKILL.md")
			writeFileForTest(t, filepath.Join(repository, foreign), []byte("---\nname: atd-tdd\ndescription: Someone else's skill.\n---\n"))
			snapshot := snapshotRepositoryForTest(t, repository)
			installFakeATDForTest(t)

			withTestRunnerDirectory(t, repository)
			err := runCLI([]string{"enable", harness.name}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), foreign) {
				t.Fatalf("enable error = %v, want refusal naming %s", err, foreign)
			}
			assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
		})
	}
}

func TestEnableIgnoresInstalledCopiesBesideSourceSkills(t *testing.T) {
	source := fstest.MapFS{
		"tdd/SKILL.md":     {Data: []byte("---\nname: tdd\ndescription: TDD.\n---\n")},
		"atd-tdd/SKILL.md": {Data: []byte("---\nname: atd-tdd\ndescription: Installed copy.\n---\n")},
	}
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	useEmbeddedSkillsForTest(t, source)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"enable", "codex"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertInstalledSkillsForTest(t, harnessesForTest[1], repository, fstest.MapFS{"tdd/SKILL.md": source["tdd/SKILL.md"]})
}

// A killed atd leaves its temporary file beside the target it was replacing.
func TestInterruptedWritesDoNotBlockUpdateOrDisable(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, command := range []string{"update", "disable"} {
			t.Run(harness.name+"/"+command, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
				// atd-review and atd-investigate were killed during their first
				// write: after creating the directory, or while filling its
				// temporary file.
				for _, name := range []string{"atd-review", "atd-investigate"} {
					if err := os.Remove(filepath.Join(repository, harness.skillsRoot, name, "SKILL.md")); err != nil {
						t.Fatal(err)
					}
				}
				leftovers := []string{
					filepath.Join(filepath.Dir(harness.hooksPath), ".atd-123456"),
					filepath.Join(harness.skillsRoot, "atd-tdd", ".atd-654321"),
					filepath.Join(harness.skillsRoot, "atd-review", ".atd-111111"),
				}
				for _, path := range leftovers {
					writeFileForTest(t, filepath.Join(repository, path), []byte("partial"))
				}
				if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
					t.Fatalf("leftover temporary files are visible to Git:\n%s", status)
				}
				if line := statusLineForTest(t, harness); line != harness.name+": repository enabled; worktree enabled; hooks current; skills missing atd-investigate, atd-review; run atd update" {
					t.Fatalf("status after interrupted writes = %q", line)
				}

				arguments := []string{command}
				if command == "disable" {
					arguments = append(arguments, harness.name)
				}
				if err := runCLI(arguments, io.Discard); err != nil {
					t.Fatal(err)
				}
				for _, path := range leftovers {
					if _, err := os.Stat(filepath.Join(repository, path)); !os.IsNotExist(err) {
						t.Fatalf("%s left %s: %v", command, path, err)
					}
				}
				if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
					t.Fatalf("%s left files visible to Git:\n%s", command, status)
				}
				if command == "update" {
					assertInstalledSkillsForTest(t, harness, repository, embeddedSourceSkillsForTest(t))
				} else if _, err := os.Stat(filepath.Join(repository, harness.skillsRoot, "atd-investigate")); !os.IsNotExist(err) {
					t.Fatalf("disable left an interrupted skill directory: %v", err)
				}
			})
		}
	}
}

// A failure partway through enable or disable must never leave atd's files
// visible to Git: enable excludes before writing, disable unexcludes last.
func TestATDFilesStayExcludedWhenWritesFail(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	for _, harness := range harnessesForTest {
		for _, command := range []string{"enable", "disable"} {
			t.Run(harness.name+"/"+command, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				skillsRoot := filepath.Join(repository, harness.skillsRoot)
				if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
					t.Fatal(err)
				}
				if command == "disable" {
					if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(skillsRoot, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(skillsRoot, 0o755) })

				if err := runCLI([]string{command, harness.name}, io.Discard); err == nil {
					t.Fatalf("%s succeeded with a read-only skills root", command)
				}
				exclude := excludeFileForTest(t, repository)
				for _, path := range []string{harness.hooksPath, harness.skillsRoot + "/atd-*/"} {
					if !strings.Contains(exclude, "/"+path+"\n") {
						t.Fatalf("failed %s left %s unexcluded:\n%s", command, path, exclude)
					}
				}
			})
		}
	}
}

func TestEnableRecordsHarnessForUnrecordedHooks(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			runTestRunnerGit(t, repository, "config", "--unset-all", "atd.harness")
			want := harness.name + ": repository not enabled; worktree enabled; hooks current; skills current; run atd enable " + harness.name
			if line := statusLineForTest(t, harness); line != want {
				t.Fatalf("status with unrecorded hooks:\n got %q\nwant %q", line, want)
			}

			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			if harnesses := runTestRunnerGit(t, repository, "config", "--local", "--get-all", "atd.harness"); harnesses != harness.name+"\n" {
				t.Fatalf("repository harness record = %q, want %q", harnesses, harness.name)
			}
			if err := runCLI([]string{"update"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			if line := statusLineForTest(t, harness); line != harness.name+": repository enabled; worktree enabled; hooks current; skills current" {
				t.Fatalf("status after recording = %q", line)
			}
		})
	}
}

func TestEnableAndUpdateRefuseTrackedHarnessFiles(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, test := range []struct {
			name    string
			command string
			track   func(*testing.T, harnessForTest, string) string
		}{
			{name: "tracked hooks file", command: "enable", track: func(t *testing.T, h harnessForTest, repository string) string {
				writeFileForTest(t, filepath.Join(repository, h.hooksPath), []byte(`{"hooks":{}}`+"\n"))
				return h.hooksPath
			}},
			{name: "tracked skill target", command: "enable", track: func(t *testing.T, h harnessForTest, repository string) string {
				path := filepath.Join(h.skillsRoot, "atd-tdd", "SKILL.md")
				rendered, err := renderSkills()
				if err != nil {
					t.Fatal(err)
				}
				writeFileForTest(t, filepath.Join(repository, path), rendered["atd-tdd"])
				return path
			}},
			{name: "installed skill tracked later", command: "update", track: func(t *testing.T, h harnessForTest, repository string) string {
				if err := runCLI([]string{"enable", h.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
				installFakeATDForTest(t)
				return filepath.Join(h.skillsRoot, "atd-review", "SKILL.md")
			}},
		} {
			t.Run(harness.name+"/"+test.name, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				tracked := test.track(t, harness, repository)
				runTestRunnerGit(t, repository, "add", "-f", "--", tracked)
				runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: track harness file")
				snapshot := snapshotRepositoryForTest(t, repository)

				arguments := []string{test.command}
				if test.command == "enable" {
					arguments = append(arguments, harness.name)
				}
				err := runCLI(arguments, io.Discard)
				if err == nil || !strings.Contains(err.Error(), tracked) {
					t.Fatalf("%s error = %v, want refusal naming tracked %s", test.command, err, tracked)
				}
				assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
			})
		}
	}
}

func TestRepositoryHarnessRecordIgnoresGlobalConfiguration(t *testing.T) {
	global := filepath.Join(t.TempDir(), "gitconfig")
	writeFileForTest(t, global, []byte("[atd]\n\tharness = codex\n"))
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	if line := statusLineForTest(t, harnessesForTest[1]); line != "codex: repository not enabled; worktree not enabled" {
		t.Fatalf("status with global atd.harness = %q", line)
	}
	if err := runCLI([]string{"enable", "codex"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if harnesses := runTestRunnerGit(t, repository, "config", "--local", "--get-all", "atd.harness"); harnesses != "codex\n" {
		t.Fatalf("local harness record = %q, want codex", harnesses)
	}
}

func TestUnknownHarnessRecordIsReportedWithoutBlockingEnableOrStatus(t *testing.T) {
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"enable", "claude"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	runTestRunnerGit(t, repository, "config", "--local", "--add", "atd.harness", "claud")
	removal := "git config --local --fixed-value --unset-all atd.harness claud"

	var output bytes.Buffer
	if err := runCLI([]string{"status"}, &output); err != nil {
		t.Fatalf("status with unknown harness record: %v", err)
	}
	if !strings.Contains(output.String(), removal) {
		t.Fatalf("status does not explain how to remove the unknown record %q:\n%s", "claud", output.String())
	}
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks current; skills current" {
		t.Fatalf("status for known harness beside unknown record = %q", line)
	}
	if err := runCLI([]string{"enable", "codex"}, io.Discard); err != nil {
		t.Fatalf("enable with unknown harness record: %v", err)
	}

	err := runCLI([]string{"update"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), removal) {
		t.Fatalf("update error = %v, want refusal naming %q", err, removal)
	}
	runTestRunnerGit(t, repository, strings.Fields(removal)[1:]...)
	if err := runCLI([]string{"update"}, io.Discard); err != nil {
		t.Fatalf("update after removing the unknown record: %v", err)
	}
}

func TestStatusReportsRepositoryAndWorktreeEnablement(t *testing.T) {
	repository := newTestRunnerRepository(t)
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	var output bytes.Buffer
	if err := runCLI([]string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^atd \S+$`).MatchString(output.String()) {
		t.Fatalf("status does not report the atd version: %q", output.String())
	}
	for _, harness := range harnessesForTest {
		if line := statusLineForTest(t, harness); line != harness.name+": repository not enabled; worktree not enabled" {
			t.Fatalf("status for unused %s = %q", harness.name, line)
		}
	}

	if err := runCLI([]string{"enable", "claude"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks current; skills current" {
		t.Fatalf("status in enabled worktree = %q", line)
	}

	installFakeATDForTest(t)
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks stale; skills current; run atd update" {
		t.Fatalf("status with hooks for another atd = %q", line)
	}

	worktree := newLinkedWorktreeForTest(t, repository)
	withTestRunnerDirectory(t, worktree)
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree not enabled; run atd enable claude" {
		t.Fatalf("status in unenabled linked worktree = %q", line)
	}
	if line := statusLineForTest(t, harnessesForTest[1]); line != "codex: repository not enabled; worktree not enabled" {
		t.Fatalf("codex status in linked worktree = %q", line)
	}

	if err := runCLI([]string{"update"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks current; skills current" {
		t.Fatalf("status after update in linked worktree = %q", line)
	}
	if line := statusLineForTest(t, harnessesForTest[1]); line != "codex: repository not enabled; worktree not enabled" {
		t.Fatalf("update enabled a harness the repository does not use: %q", line)
	}
}

func TestClaudeAgentWorktreesStayOutOfCandidateGates(t *testing.T) {
	repository := newVerifierRepository(t)
	base := verifierHead(t, repository)
	agentDefinition := filepath.Join(repository, ".claude", "agents", "reviewer.md")
	writeFileForTest(t, agentDefinition, []byte("---\nname: reviewer\ndescription: Host-owned reviewer.\n---\n"))
	candidate := commitAll(t, repository, "chore: add host reviewer")
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"enable", "claude"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	runTestRunnerGit(t, repository, "worktree", "add", "-q", "--detach", filepath.Join(repository, ".claude", "worktrees", "agent-reviewer"))

	recordVerifierTest(t, repository, testModeFast, false)
	recordVerifierTest(t, repository, testModeFull, false)
	if err := verifyCandidate(repository, base, candidate, io.Discard); err != nil {
		t.Fatalf("verify with a Claude agent worktree present: %v", err)
	}
	if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("Claude agent worktree is visible to Git:\n%s", status)
	}
	writeFileForTest(t, agentDefinition, []byte("---\nname: reviewer\ndescription: Edited host-owned reviewer.\n---\n"))
	writeFileForTest(t, filepath.Join(repository, ".claude", "agents", "implementer.md"), []byte("---\nname: implementer\ndescription: New host-owned implementer.\n---\n"))
	if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != " M .claude/agents/reviewer.md\n?? .claude/agents/implementer.md\n" {
		t.Fatalf("host-owned agent definitions are not visible to Git:\n%s", status)
	}
}

func TestUpdateAddsNewClaudeExcludesAndPreservesDeveloperExcludes(t *testing.T) {
	repository := newTestRunnerRepository(t)
	developerExcludes := excludeFileForTest(t, repository) + "/local-notes/\n"
	exclude := filepath.Join(repository, ".git", "info", "exclude")
	writeFileForTest(t, exclude, []byte(developerExcludes))
	installFakeATDForTest(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"enable", "claude"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	enabled := excludeFileForTest(t, repository)
	withoutWorktrees := strings.Replace(enabled, "/.claude/worktrees/\n", "", 1)
	if withoutWorktrees == enabled {
		t.Fatalf("enable did not exclude Claude agent worktrees:\n%s", enabled)
	}
	// An installation from before atd excluded Claude agent worktrees, followed
	// by a line the developer added later.
	withoutWorktrees += "/after-atd/\n"
	writeFileForTest(t, exclude, []byte(withoutWorktrees))
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks current; skills current; excludes missing /.claude/worktrees/; run atd update" {
		t.Fatalf("status with a missing exclude = %q", line)
	}

	if err := runCLI([]string{"update"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	runTestRunnerGit(t, repository, "check-ignore", "-q", "--no-index", "--", ".claude/worktrees/agent-reviewer/tracked.txt")
	if updated := excludeFileForTest(t, repository); !strings.HasPrefix(updated, withoutWorktrees) {
		t.Fatalf("update rewrote existing excludes:\n%s\nwant prefix:\n%s", updated, withoutWorktrees)
	}
	if line := statusLineForTest(t, harnessesForTest[0]); line != "claude: repository enabled; worktree enabled; hooks current; skills current" {
		t.Fatalf("status after update = %q", line)
	}

	if err := runCLI([]string{"disable", "claude"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, want := excludeFileForTest(t, repository), developerExcludes+"/after-atd/\n"; got != want {
		t.Fatalf("disable left excludes:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateRequiresEnabledHarness(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	err := runCLI([]string{"update"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "atd enable") {
		t.Fatalf("update without enabled harness error = %v", err)
	}
}

func TestHarnessCommandSurface(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	for _, arguments := range [][]string{{"init", "codex"}, {"enable"}, {"enable", "opencode"}, {"enable", "claude", "codex"}, {"update", "claude"}, {"status", "claude"}} {
		if err := runCLI(arguments, io.Discard); err == nil {
			t.Fatalf("atd %s succeeded", strings.Join(arguments, " "))
		}
	}
	if _, err := os.Stat(filepath.Join(repository, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("rejected command installed Codex files: %v", err)
	}
}

func TestOrdinaryInitDoesNotEnableHarnesses(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"init"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".codex", ".claude", ".agents"} {
		if _, err := os.Stat(filepath.Join(repository, path)); !os.IsNotExist(err) {
			t.Fatalf("ordinary init created %s: %v", path, err)
		}
	}
}

func TestClaudeHookDeliversSteeringContext(t *testing.T) {
	repository := newTestRunnerRepository(t)
	writeTestRunnerFile(t, filepath.Join(repository, "source.go"), "package source\n")
	withTestRunnerDirectory(t, repository)
	var output bytes.Buffer
	for attempt := 1; attempt <= 3; attempt++ {
		output.Reset()
		if err := runCLI([]string{"_claude-hook", "edit"}, &output); err != nil {
			t.Fatal(err)
		}
		if attempt < 3 && output.Len() != 0 {
			t.Fatalf("edit %d produced early context: %q", attempt, output.String())
		}
	}
	var response struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("decode hook output: %v\n%s", err, output.String())
	}
	if response.HookSpecificOutput.HookEventName != "PreToolUse" || !strings.Contains(response.HookSpecificOutput.AdditionalContext, "atd test fast") {
		t.Fatalf("hook response = %#v", response)
	}
}

func TestHookContextIsJSONEncoded(t *testing.T) {
	context := "quote=\" slash=\\ newline=\nend"
	var output bytes.Buffer
	if err := writeHookContext(&output, context); err != nil {
		t.Fatal(err)
	}
	var response struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.HookSpecificOutput.AdditionalContext != context {
		t.Fatalf("additional context = %q, want %q", response.HookSpecificOutput.AdditionalContext, context)
	}
}

func existingHookConfigurationForTest(harness harnessForTest) []byte {
	if harness.name == "claude" {
		return []byte(`{
  "permissions": {"allow": ["Bash(make test && make lint)"], "large": 123456789012345678901234567890},
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "custom": {"preserve": true},
        "hooks": [{"type": "command", "command": "user-pre-hook", "timeout": 17}]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{"type": "command", "command": "user-post-hook"}]
      }
    ]
  }
}
`)
	}
	return []byte(`{
  "description": "user-owned hooks: make test && make lint",
  "user_setting": {"preserve": true, "large": 123456789012345678901234567890},
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^Bash$",
        "custom": {"preserve": true},
        "hooks": [{"type": "command", "command": "user-pre-hook", "timeout": 17}]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "^Bash$",
        "hooks": [{"type": "command", "command": "user-post-hook"}]
      }
    ]
  }
}
`)
}

// Claude Code treats matchers made only of word characters and "|" as exact
// tool-name lists and anything else as a regular expression.
func claudeMatcherSelectsForTest(t *testing.T, matcher, tool string) bool {
	t.Helper()
	if matcher == "" || matcher == "*" {
		return true
	}
	if regexp.MustCompile(`^[A-Za-z0-9_|]+$`).MatchString(matcher) {
		for _, name := range strings.Split(matcher, "|") {
			if name == tool {
				return true
			}
		}
		return false
	}
	return regexpMatcherSelectsForTest(t, matcher, tool)
}

func regexpMatcherSelectsForTest(t *testing.T, matcher, tool string) bool {
	t.Helper()
	pattern, err := regexp.Compile(matcher)
	if err != nil {
		t.Fatalf("invalid PreToolUse matcher %q: %v", matcher, err)
	}
	return pattern.MatchString(tool)
}

func assertExistingHookConfigurationForTest(t *testing.T, before, after map[string]any) {
	t.Helper()
	if before == nil {
		return
	}
	for key, value := range before {
		if key != "hooks" && !reflect.DeepEqual(after[key], value) {
			t.Fatalf("existing top-level field %q changed:\nbefore: %#v\nafter:  %#v", key, value, after[key])
		}
	}
	beforeHooks := objectForTest(t, before["hooks"], "existing hooks")
	afterHooks := objectForTest(t, after["hooks"], "installed hooks")
	for event, value := range beforeHooks {
		if event != "PreToolUse" && !reflect.DeepEqual(afterHooks[event], value) {
			t.Fatalf("existing %s hooks changed:\nbefore: %#v\nafter:  %#v", event, value, afterHooks[event])
		}
	}
	for _, beforeValue := range arrayForTest(t, beforeHooks["PreToolUse"], "existing PreToolUse") {
		beforeGroup := objectForTest(t, beforeValue, "existing PreToolUse group")
		preserved := false
		for _, afterValue := range arrayForTest(t, afterHooks["PreToolUse"], "installed PreToolUse") {
			if hookGroupPreservesForTest(t, beforeGroup, objectForTest(t, afterValue, "installed PreToolUse group")) {
				preserved = true
				break
			}
		}
		if !preserved {
			t.Fatalf("existing PreToolUse group changed or disappeared: %#v", beforeGroup)
		}
	}
}

func assertInstalledHooksForTest(t *testing.T, harness harnessForTest, repository string, before, after map[string]any, marker string) {
	t.Helper()
	hooks := objectForTest(t, after["hooks"], "hooks")
	allowedEvents := map[string]bool{"PreToolUse": true}
	if before != nil {
		for event := range objectForTest(t, before["hooks"], "existing hooks") {
			allowedEvents[event] = true
		}
	}
	if len(hooks) != len(allowedEvents) {
		t.Fatalf("enable added unexpected lifecycle events: %#v", hooks)
	}
	for event := range hooks {
		if !allowedEvents[event] {
			t.Fatalf("enable added unexpected %s hooks", event)
		}
	}
	preToolUse := arrayForTest(t, hooks["PreToolUse"], "PreToolUse")
	existingHandlers := existingPreToolHandlersForTest(t, before)
	afterHandlerCount := 0
	nestedDirectory := filepath.Join(repository, "nested", "session")
	if err := os.MkdirAll(nestedDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	matched := map[string]int{"command": 0, "edit": 0}
	for _, value := range preToolUse {
		group := objectForTest(t, value, "PreToolUse group")
		handlers := arrayForTest(t, group["hooks"], "PreToolUse handlers")
		afterHandlerCount += len(handlers)
		var installedHandlers []any
		for _, handlerValue := range handlers {
			if !containsDeepEqualForTest(existingHandlers, handlerValue) {
				installedHandlers = append(installedHandlers, handlerValue)
			}
		}
		if len(installedHandlers) == 0 {
			if !containsDeepEqualForTest(existingPreToolGroupsForTest(t, before), value) {
				t.Fatalf("enable added a PreToolUse group without an ATD handler: %#v", group)
			}
			continue
		}
		matcher := ""
		if matcherValue, present := group["matcher"]; present {
			var ok bool
			matcher, ok = matcherValue.(string)
			if !ok {
				t.Fatalf("PreToolUse matcher is not a string: %#v", matcherValue)
			}
		}
		commandMatch := harness.matcherSelects(t, matcher, "Bash")
		editMatches := 0
		for _, tool := range harness.editTools {
			if harness.matcherSelects(t, matcher, tool) {
				editMatches++
			}
		}
		if commandMatch == (editMatches > 0) || editMatches > 0 && editMatches != len(harness.editTools) {
			t.Fatalf("installed matcher %q must select exactly one complete trigger family", matcher)
		}
		for _, unrelated := range harness.unrelatedTools {
			if harness.matcherSelects(t, matcher, unrelated) {
				t.Fatalf("installed matcher %q also selects unrelated tool %q", matcher, unrelated)
			}
		}
		trigger := "edit"
		if commandMatch {
			trigger = "command"
		}
		for _, handlerValue := range installedHandlers {
			handler := objectForTest(t, handlerValue, "PreToolUse handler")
			if handler["type"] != "command" {
				t.Fatalf("installed PreToolUse handler is not a command: %#v", handler)
			}
			if async, present := handler["async"]; present && async != false {
				t.Fatalf("installed PreToolUse handler is asynchronous: %#v", handler)
			}
			command, _ := handler["command"].(string)
			if command == "" {
				t.Fatalf("installed PreToolUse handler has no command: %#v", handler)
			}
			assertHookDeliversContextForTest(t, nestedDirectory, command, marker+"-"+harness.hookSubcommand+"-"+trigger)
			matched[trigger]++
		}
	}
	for trigger, count := range matched {
		if count != 1 {
			t.Fatalf("installed %s hook matches = %d, want 1", trigger, count)
		}
	}
	if afterHandlerCount != len(existingHandlers)+2 {
		t.Fatalf("PreToolUse handler count = %d, want %d existing + 2 ATD", afterHandlerCount, len(existingHandlers))
	}
}

func assertHookDeliversContextForTest(t *testing.T, directory, command, want string) {
	t.Helper()
	hook := exec.Command("sh", "-c", command)
	hook.Dir = directory
	hook.Stdin = strings.NewReader(`{"tool_name":"payload is ignored"}`)
	output, err := hook.CombinedOutput()
	if err != nil {
		t.Fatalf("run hook from nested directory: %v\n%s", err, output)
	}
	var response struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook output: %v\n%s", err, output)
	}
	if response.HookSpecificOutput.HookEventName != "PreToolUse" || response.HookSpecificOutput.AdditionalContext != want {
		t.Fatalf("hook response = %#v, want context %q", response, want)
	}
}

// Installed skills must be the source skills with only their names and
// cross-skill links namespaced, followed by one trailing ownership comment.
func assertInstalledSkillsForTest(t *testing.T, harness harnessForTest, worktree string, source fs.FS) {
	t.Helper()
	root := filepath.Join(worktree, harness.skillsRoot)
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, entry := range entries {
		want = append(want, "atd-"+entry.Name())
	}
	var installed []string
	directories, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range directories {
		if strings.HasPrefix(directory.Name(), "atd-") {
			installed = append(installed, directory.Name())
		}
	}
	sort.Strings(installed)
	if !reflect.DeepEqual(installed, want) {
		t.Fatalf("installed skills = %v, want %v", installed, want)
	}
	link := regexp.MustCompile(`\]\(([^)#]+)(#[^)]*)?\)`)
	for _, entry := range entries {
		name := entry.Name()
		sourceContents, err := fs.ReadFile(source, name+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "atd-"+name, "SKILL.md")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body, marker, found := cutLastLineForTest(string(contents))
		if !found || !strings.HasPrefix(marker, "<!-- atd") || !strings.HasSuffix(marker, "-->") {
			t.Fatalf("%s lacks a trailing ownership comment: %q", path, marker)
		}
		if !strings.Contains(body, "\nname: atd-"+name+"\n") {
			t.Fatalf("%s does not rename its skill to atd-%s", path, name)
		}
		restored := strings.Replace(body, "\nname: atd-"+name+"\n", "\nname: "+name+"\n", 1)
		restored = strings.ReplaceAll(restored, "](../atd-", "](../")
		if restored != string(sourceContents) {
			t.Fatalf("%s content differs from source skill %s:\n%s", path, name, body)
		}
		for _, match := range link.FindAllStringSubmatch(body, -1) {
			target := match[1]
			if strings.Contains(target, "://") {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
			if !strings.HasPrefix(resolved, filepath.Join(root, "atd-")) {
				t.Fatalf("%s links outside installed atd skills: %s", path, target)
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Fatalf("%s has broken link %s: %v", path, target, err)
			}
		}
	}
}

func cutLastLineForTest(contents string) (string, string, bool) {
	trimmed := strings.TrimSuffix(contents, "\n")
	index := strings.LastIndex(trimmed, "\n")
	if index < 0 {
		return "", "", false
	}
	return trimmed[:index+1], trimmed[index+1:], true
}

// A Codex-enabled ATD checkout also holds installed atd-* copies beside the
// source skills; they are not sources.
func embeddedSourceSkillsForTest(t *testing.T) fs.FS {
	t.Helper()
	source := fstest.MapFS{}
	for _, name := range sourceSkillNamesForTest {
		contents, err := os.ReadFile(filepath.Join(sourceSkillsDirectoryForTest, name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		source[name+"/SKILL.md"] = &fstest.MapFile{Data: contents}
	}
	entries, err := os.ReadDir(sourceSkillsDirectoryForTest)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "atd-") {
			names = append(names, entry.Name())
		}
	}
	if !reflect.DeepEqual(names, sourceSkillNamesForTest) {
		t.Fatalf("source skills = %v, want %v", names, sourceSkillNamesForTest)
	}
	return source
}

func previousReleaseSkillsForTest() fs.FS {
	return fstest.MapFS{
		"evaluate-review/SKILL.md": {Data: []byte("---\nname: evaluate-review\ndescription: Old evaluation.\n---\n\nUse [tdd](../tdd/SKILL.md).\n")},
		"investigate/SKILL.md":     {Data: []byte("---\nname: investigate\ndescription: Old investigation.\n---\n\n# Investigate a change\n\nThen use [tdd](../tdd/SKILL.md).\n")},
		"retired/SKILL.md":         {Data: []byte("---\nname: retired\ndescription: A skill removed upstream.\n---\n\nRetired body.\n")},
		"tdd/SKILL.md":             {Data: []byte("---\nname: tdd\ndescription: Old TDD.\n---\n\nOld cycle.\n")},
	}
}

func useEmbeddedSkillsForTest(t *testing.T, skills fs.FS) {
	t.Helper()
	previous := embeddedSkills
	embeddedSkills = skills
	t.Cleanup(func() { embeddedSkills = previous })
}

func statusLineForTest(t *testing.T, harness harnessForTest) string {
	t.Helper()
	var output bytes.Buffer
	if err := runCLI([]string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, harness.name+":") {
			return line
		}
	}
	t.Fatalf("status has no %s line:\n%s", harness.name, output.String())
	return ""
}

func newLinkedWorktreeForTest(t *testing.T, repository string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(parent, "worktree")
	runTestRunnerGit(t, repository, "worktree", "add", "-q", worktree)
	return worktree
}

// The snapshot covers every worktree file plus the Git configuration and
// exclude file that enable may write; Git's own index refreshes are ignored.
func snapshotRepositoryForTest(t *testing.T, repository string) map[string]string {
	t.Helper()
	snapshot := snapshotWorkingFilesForTest(t, repository)
	common := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	for _, name := range []string{"config", "info/exclude"} {
		contents, err := os.ReadFile(filepath.Join(common, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		snapshot["<git>/"+name] = string(contents)
	}
	return snapshot
}

func snapshotWorkingFilesForTest(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot[relative] = "symlink:" + target
		case entry.IsDir():
			snapshot[relative+"/"] = ""
		default:
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot[relative] = string(contents)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSnapshotUnchangedForTest(t *testing.T, before, after map[string]string) {
	t.Helper()
	for path, contents := range before {
		if afterContents, ok := after[path]; !ok || afterContents != contents {
			t.Fatalf("%s changed:\nbefore: %q\nafter:  %q (present=%t)", path, contents, afterContents, ok)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Fatalf("%s was created", path)
		}
	}
}

func writeFileForTest(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSONForTest(t *testing.T, path string) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeJSONForTest(t, path, contents)
}

func decodeJSONForTest(t *testing.T, path string, contents []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, contents)
	}
	return value
}

func objectForTest(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, value)
	}
	return object
}

func arrayForTest(t *testing.T, value any, name string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", name, value)
	}
	return array
}

func hookGroupPreservesForTest(t *testing.T, before, after map[string]any) bool {
	t.Helper()
	for key, value := range before {
		if key != "hooks" && !reflect.DeepEqual(after[key], value) {
			return false
		}
	}
	afterHandlers := arrayForTest(t, after["hooks"], "installed hook handlers")
	for _, beforeHandler := range arrayForTest(t, before["hooks"], "existing hook handlers") {
		if !containsDeepEqualForTest(afterHandlers, beforeHandler) {
			return false
		}
	}
	return true
}

func existingPreToolHandlersForTest(t *testing.T, configuration map[string]any) []any {
	t.Helper()
	var handlers []any
	for _, groupValue := range existingPreToolGroupsForTest(t, configuration) {
		group := objectForTest(t, groupValue, "existing PreToolUse group")
		handlers = append(handlers, arrayForTest(t, group["hooks"], "existing PreToolUse handlers")...)
	}
	return handlers
}

func existingPreToolGroupsForTest(t *testing.T, configuration map[string]any) []any {
	t.Helper()
	if configuration == nil {
		return nil
	}
	hooks := objectForTest(t, configuration["hooks"], "existing hooks")
	return arrayForTest(t, hooks["PreToolUse"], "existing PreToolUse")
}

func containsDeepEqualForTest(values []any, wanted any) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, wanted) {
			return true
		}
	}
	return false
}

func installFakeATDForTest(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	marker := "fake-atd-" + hex.EncodeToString(random)
	fakeATD := "#!/bin/sh\n[ \"$#\" = 2 ] || exit 23\nprintf '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"" + marker + "-%s-%s\"}}\\n' \"$1\" \"$2\"\n"
	path := filepath.Join(bin, "atd")
	if err := os.WriteFile(path, []byte(fakeATD), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := currentExecutable
	currentExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { currentExecutable = previous })
	return marker
}

func assertFileContentsForTest(t *testing.T, path string, want []byte) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, want) {
		t.Fatalf("%s = %q, want %q", path, contents, want)
	}
}
