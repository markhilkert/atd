package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDisableRestoresWorktreeToItsStateBeforeEnable(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, test := range []struct {
			name     string
			existing []byte
			excluded bool
		}{
			{name: "pristine repository"},
			{name: "existing hooks", existing: existingHookConfigurationForTest(harness)},
			{name: "settings without hooks", existing: []byte(`{"permissions": {"allow": ["Bash(make test && make lint)"]}}` + "\n")},
			{name: "hooks file the developer excludes", existing: existingHookConfigurationForTest(harness), excluded: true},
		} {
			t.Run(harness.name+"/"+test.name, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				hostSkill := filepath.Join(repository, harness.skillsRoot, "host-skill", "SKILL.md")
				writeFileForTest(t, hostSkill, []byte("---\nname: host-skill\ndescription: Host-owned skill.\n---\n"))
				runTestRunnerGit(t, repository, "add", "--", harness.skillsRoot)
				runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: add host skill")
				hooksPath := filepath.Join(repository, harness.hooksPath)
				if test.existing != nil {
					writeFileForTest(t, hooksPath, test.existing)
				}
				if test.excluded {
					exclude := filepath.Join(repository, ".git", "info", "exclude")
					writeFileForTest(t, exclude, []byte(excludeFileForTest(t, repository)+"/"+harness.hooksPath+"\n"))
				}
				before := snapshotRepositoryForTest(t, repository)
				statusBefore := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all")
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
					t.Fatal(err)
				}

				var output bytes.Buffer
				if err := runCLI([]string{"disable", harness.name}, &output); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(output.String(), "disabled atd") {
					t.Fatalf("disable output = %q", output.String())
				}

				after := snapshotRepositoryForTest(t, repository)
				if test.existing == nil {
					if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
						t.Fatalf("disable left a hooks file atd created: %v", err)
					}
				} else {
					want := decodeJSONForTest(t, hooksPath, test.existing)
					if got := readJSONForTest(t, hooksPath); !reflect.DeepEqual(got, want) {
						t.Fatalf("disable did not restore existing hooks:\n got %#v\nwant %#v", got, want)
					}
					delete(before, harness.hooksPath)
					delete(after, harness.hooksPath)
				}
				delete(before, "<git>/config")
				delete(after, "<git>/config")
				assertSnapshotUnchangedForTest(t, withoutDirectoriesForTest(before), withoutDirectoriesForTest(after))
				if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != statusBefore {
					t.Fatalf("Git status after disable:\n%s\nwant:\n%s", status, statusBefore)
				}
				assertHarnessUnrecordedForTest(t, repository)
				if line := statusLineForTest(t, harness); line != harness.name+": repository not enabled; worktree not enabled" {
					t.Fatalf("status after disable = %q", line)
				}
			})
		}
	}
}

func TestDisableRefusesEditedSkillsUnlessForced(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
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
			snapshot := snapshotRepositoryForTest(t, repository)

			err = runCLI([]string{"disable", harness.name}, io.Discard)
			if err == nil {
				t.Fatal("disable removed hand-edited skills without --force")
			}
			for _, want := range []string{edited, foreign, "--force"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("disable error %q does not mention %s", err, want)
				}
			}
			assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))

			if err := runCLI([]string{"disable", "--force", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(repository, harness.skillsRoot))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, entry := range entries {
				t.Fatalf("forced disable left %s", filepath.Join(harness.skillsRoot, entry.Name()))
			}
			if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("forced disable left files visible to Git:\n%s", status)
			}
			assertHarnessUnrecordedForTest(t, repository)
		})
	}
}

func TestDisableRefusesBeforeWritingAnything(t *testing.T) {
	for _, harness := range harnessesForTest {
		for _, test := range []struct {
			name    string
			prepare func(*testing.T, harnessForTest, string) string
		}{
			{name: "malformed hooks", prepare: func(t *testing.T, h harnessForTest, repository string) string {
				if err := runCLI([]string{"enable", h.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
				writeFileForTest(t, filepath.Join(repository, h.hooksPath), []byte("{\"hooks\":"))
				return h.hooksPath
			}},
			{name: "tracked hooks with atd handlers", prepare: func(t *testing.T, h harnessForTest, repository string) string {
				if err := runCLI([]string{"enable", h.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
				runTestRunnerGit(t, repository, "add", "-f", "--", h.hooksPath)
				runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: track hooks with atd handlers")
				return h.hooksPath
			}},
			{name: "tracked skill", prepare: func(t *testing.T, h harnessForTest, repository string) string {
				if err := runCLI([]string{"enable", h.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
				tracked := filepath.Join(h.skillsRoot, "atd-review", "SKILL.md")
				runTestRunnerGit(t, repository, "add", "-f", "--", tracked)
				runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: track installed skill")
				return tracked
			}},
		} {
			t.Run(harness.name+"/"+test.name, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				path := test.prepare(t, harness, repository)
				snapshot := snapshotRepositoryForTest(t, repository)

				err := runCLI([]string{"disable", "--force", harness.name}, io.Discard)
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("disable error = %v, want refusal naming %s", err, path)
				}
				assertSnapshotUnchangedForTest(t, snapshot, snapshotRepositoryForTest(t, repository))
			})
		}
	}
}

func TestDisableLeavesTrackedHooksWithoutATDHandlers(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			hooksPath := filepath.Join(repository, harness.hooksPath)
			team := existingHookConfigurationForTest(harness)
			writeFileForTest(t, hooksPath, team)
			runTestRunnerGit(t, repository, "add", "-f", "--", harness.hooksPath)
			runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: track team hooks")

			if err := runCLI([]string{"disable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assertFileContentsForTest(t, hooksPath, team)
			if _, err := os.Stat(filepath.Join(repository, harness.skillsRoot, "atd-tdd")); !os.IsNotExist(err) {
				t.Fatalf("disable left skills beside tracked team hooks: %v", err)
			}
		})
	}
}

func TestDisableKeepsSharedStateWhileAnotherWorktreeHasHarness(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			worktree := newLinkedWorktreeForTest(t, repository)
			removed := filepath.Join(filepath.Dir(worktree), "removed")
			runTestRunnerGit(t, repository, "worktree", "add", "-q", removed)
			for _, directory := range []string{repository, worktree, removed} {
				withTestRunnerDirectory(t, directory)
				if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.RemoveAll(removed); err != nil {
				t.Fatal(err)
			}
			exclude := excludeFileForTest(t, repository)

			withTestRunnerDirectory(t, worktree)
			var output bytes.Buffer
			if err := runCLI([]string{"disable", harness.name}, &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), repository) {
				t.Fatalf("disable output does not name the worktree still using %s: %q", harness.name, output.String())
			}
			if strings.Contains(output.String(), removed) {
				t.Fatalf("disable counted a deleted worktree as using %s: %q", harness.name, output.String())
			}
			if _, err := os.Stat(filepath.Join(worktree, harness.hooksPath)); !os.IsNotExist(err) {
				t.Fatalf("disable left hooks in its worktree: %v", err)
			}
			if got := excludeFileForTest(t, repository); got != exclude {
				t.Fatalf("disable changed shared excludes while %s still uses them:\n%s", repository, got)
			}
			if harnesses := runTestRunnerGit(t, repository, "config", "--local", "--get-all", "atd.harness"); harnesses != harness.name+"\n" {
				t.Fatalf("repository harness record = %q, want %q", harnesses, harness.name)
			}
			withTestRunnerDirectory(t, repository)
			if line := statusLineForTest(t, harness); line != harness.name+": repository enabled; worktree enabled; hooks current; skills current" {
				t.Fatalf("status in remaining worktree = %q", line)
			}
			if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("remaining worktree's atd files became visible to Git:\n%s", status)
			}

			if err := runCLI([]string{"disable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(excludeFileForTest(t, repository), "atd-") {
				t.Fatalf("last disable left atd exclude entries:\n%s", excludeFileForTest(t, repository))
			}
			assertHarnessUnrecordedForTest(t, repository)
		})
	}
}

func TestDisableKeepsExcludesHidingAnotherWorktreesSkills(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			worktree := newLinkedWorktreeForTest(t, repository)
			for _, directory := range []string{repository, worktree} {
				withTestRunnerDirectory(t, directory)
				if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(repository, harness.hooksPath)); err != nil {
				t.Fatal(err)
			}

			if err := runCLI([]string{"disable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			if status := runTestRunnerGit(t, repository, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("disable exposed skills another worktree still has:\n%s", status)
			}
		})
	}
}

func TestDisableClearsRepositoryRecordNoWorktreeUses(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{harness.hooksPath, harness.skillsRoot} {
				if err := os.RemoveAll(filepath.Join(repository, path)); err != nil {
					t.Fatal(err)
				}
			}
			other := harnessesForTest[0]
			if other.name == harness.name {
				other = harnessesForTest[1]
			}
			runTestRunnerGit(t, repository, "config", "--local", "--add", "atd.harness", other.name)

			var output bytes.Buffer
			if err := runCLI([]string{"disable", harness.name}, &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "not enabled") {
				t.Fatalf("disable output does not say the worktree was not enabled: %q", output.String())
			}
			if harnesses := runTestRunnerGit(t, repository, "config", "--local", "--get-all", "atd.harness"); harnesses != other.name+"\n" {
				t.Fatalf("repository harness record = %q, want only %q", harnesses, other.name)
			}
			if strings.Contains(excludeFileForTest(t, repository), harness.hooksPath) {
				t.Fatalf("disable left %s exclude entries:\n%s", harness.name, excludeFileForTest(t, repository))
			}
		})
	}
}

func TestDisableCommandSurface(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	for _, arguments := range [][]string{{"disable"}, {"disable", "opencode"}, {"disable", "claude", "codex"}, {"disable", "--force"}, {"disable", "claude", "--force", "--force"}, {"disable", "claude", "--all"}} {
		if err := runCLI(arguments, io.Discard); err == nil {
			t.Fatalf("atd %s succeeded", strings.Join(arguments, " "))
		}
	}
}

func assertHarnessUnrecordedForTest(t *testing.T, repository string) {
	t.Helper()
	output, err := exec.Command("git", "-C", repository, "config", "--local", "--get-all", "atd.harness").Output()
	if err == nil || len(output) != 0 {
		t.Fatalf("repository still records atd.harness %q (err=%v)", output, err)
	}
}

func excludeFileForTest(t *testing.T, repository string) string {
	t.Helper()
	common := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	contents, err := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(contents)
}

// Git does not track directories, so directories enable created and disable
// leaves empty are invisible to the repository.
func withoutDirectoriesForTest(snapshot map[string]string) map[string]string {
	files := map[string]string{}
	for path, contents := range snapshot {
		if !strings.HasSuffix(path, "/") {
			files[path] = contents
		}
	}
	return files
}
