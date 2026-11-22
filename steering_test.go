package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSteeringHookNudgesOnceAtSourceEditThreshold(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer

	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)

	want := "atd: source edits are accumulating without an observed test; run atd test fast\n"
	if output.String() != want {
		t.Fatalf("threshold output = %q, want %q", output.String(), want)
	}
	output.Reset()
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "third\n")
	for range 3 {
		runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("repeated source advisory = %q", output.String())
	}
}

func TestSteeringHookCountsOnlyOneCleanEditIntent(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	for range 4 {
		runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("clean edit advisory = %q", output.String())
	}
	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := nonemptyLines(contents); len(lines) != 1 {
		t.Fatalf("clean edit records = %d, want 1", len(lines))
	}
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if !strings.Contains(output.String(), "run atd test fast") {
		t.Fatalf("source threshold after clean intent = %q", output.String())
	}
}

func TestSteeringHookResetsAfterObservedTest(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer

	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if output.Len() == 0 {
		t.Fatal("source edit threshold did not nudge")
	}

	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "third\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if output.Len() != 0 {
		t.Fatalf("advisory did not reset after observed test: %q", output.String())
	}
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "fourth\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if !strings.Contains(output.String(), "run atd test fast") {
		t.Fatalf("reset threshold did not nudge again: %q", output.String())
	}
}

func TestSteeringHookDistinguishesTestOnlyChanges(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer

	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature_test.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature_test.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)

	want := "atd: run the changed test with atd test fast or atd test -- <command> and confirm the intended red\n"
	if output.String() != want {
		t.Fatalf("test-only output = %q, want %q", output.String(), want)
	}
}

func TestSteeringCommandHookNudgesOnceAfterThreeCommands(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer

	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "changed\n")
	for range 2 {
		runSteeringHookForTest(t, repository, steeringTriggerCommand, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("early command advisory = %q", output.String())
	}
	runSteeringHookForTest(t, repository, steeringTriggerCommand, &output)
	want := "atd: if this command runs tests, use atd test fast, full, or -- <command> so the result is observed\n"
	if output.String() != want {
		t.Fatalf("command output = %q, want %q", output.String(), want)
	}
	output.Reset()
	for range 3 {
		runSteeringHookForTest(t, repository, steeringTriggerCommand, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("repeated command advisory = %q", output.String())
	}
}

// Hooks run before every edit and command, so the working-tree scan is their
// dominant cost in large repositories.
func TestSteeringHookScansWorktreeOnceAndNotAfterThreshold(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repository := newTestRunnerRepository(t)
	log := filepath.Join(t.TempDir(), "git.log")
	installFakeGit(t, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+log+"'\nexec '"+realGit+"' \"$@\"\n")
	scans := func() int {
		t.Helper()
		contents, err := os.ReadFile(log)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		count := 0
		for _, line := range strings.Split(string(contents), "\n") {
			if fields := strings.Fields(line); len(fields) > 2 && (fields[2] == "status" || fields[2] == "diff" || fields[2] == "ls-files") {
				count++
			}
		}
		return count
	}
	var output bytes.Buffer
	for i, trigger := range []steeringTrigger{steeringTriggerEdit, steeringTriggerEdit, steeringTriggerEdit, steeringTriggerCommand, steeringTriggerCommand, steeringTriggerCommand} {
		writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), strings.Repeat("x", i+1)+"\n")
		runSteeringHookForTest(t, repository, trigger, &output)
		if n := scans(); n != 1 {
			t.Fatalf("%s hook %d below threshold scanned the worktree %d times, want 1", trigger, i+1, n)
		}
	}
	if strings.Count(output.String(), "atd:") != 2 {
		t.Fatalf("threshold output = %q, want one edit and one command advisory", output.String())
	}
	for _, trigger := range []steeringTrigger{steeringTriggerEdit, steeringTriggerCommand} {
		runSteeringHookForTest(t, repository, trigger, &output)
		if n := scans(); n != 0 {
			t.Fatalf("%s hook past its threshold scanned the worktree %d times", trigger, n)
		}
	}
}

func TestSteeringHookClassifiesStagedRenamesByNewPath(t *testing.T) {
	repository := newTestRunnerRepository(t)
	runTestRunnerGit(t, repository, "mv", "tracked.txt", "notes.md")
	var output bytes.Buffer
	for range 4 {
		runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
		runSteeringHookForTest(t, repository, steeringTriggerCommand, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("documentation rename advisory = %q", output.String())
	}
}

func TestSteeringCommandHookIgnoresCleanTree(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	for range 4 {
		runSteeringHookForTest(t, repository, steeringTriggerCommand, &output)
	}
	if output.Len() != 0 {
		t.Fatalf("clean-tree command advisory = %q", output.String())
	}
}

func TestSteeringHookRecognizesCommonTestPaths(t *testing.T) {
	tests := []string{
		"tests/feature.py",
		"test_feature.py",
		"feature_test.py",
		"src/feature.spec.ts",
		"spec/feature_spec.rb",
		"__tests__/feature.ts",
		"src/__tests__/feature.ts",
		"src/FeatureTest.java",
		"script/test",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			fullPath := filepath.Join(repository, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
			writeTestRunnerFile(t, fullPath, "changed\n")
			runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
			writeTestRunnerFile(t, fullPath, "changed again\n")
			runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
			if !strings.Contains(output.String(), "confirm the intended red") || strings.Contains(output.String(), "source edits") {
				t.Fatalf("test-path output = %q", output.String())
			}
		})
	}
}

func TestSteeringHookIgnoresDocumentationOnlyChanges(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "README.md"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "README.md"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if output.Len() != 0 {
		t.Fatalf("documentation-only advisory = %q", output.String())
	}
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if !strings.Contains(output.String(), "run atd test fast") {
		t.Fatalf("documentation edits consumed source threshold: %q", output.String())
	}
}

func TestSteeringStateIsMetadataOnlyAppendOnlyJSONL(t *testing.T) {
	repository := newTestRunnerRepository(t)
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &bytes.Buffer{})
	writeTestRunnerFile(t, filepath.Join(repository, "secret-feature.go"), "DO_NOT_PERSIST\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &bytes.Buffer{})

	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(contents), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("state lines = %d, want 2: %q", len(lines), contents)
	}
	for _, line := range lines {
		var event steeringEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("state line is not JSON: %v", err)
		}
	}
	if bytes.Contains(contents, []byte("secret-feature.go")) || bytes.Contains(contents, []byte("DO_NOT_PERSIST")) {
		t.Fatalf("state persisted source details: %q", contents)
	}
}

func TestSteeringUsesTheObservationIndexAsItsOnlyStateFile(t *testing.T) {
	repository := newTestRunnerRepository(t)
	steeringPath, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	indexPath, err := testObservationIndexPath(repository)
	if err != nil {
		t.Fatal(err)
	}
	if steeringPath != indexPath {
		t.Fatalf("steering state = %q, observation index = %q", steeringPath, indexPath)
	}
}

func TestSteeringDoesNotWriteStateForCleanCommands(t *testing.T) {
	repository := newTestRunnerRepository(t)
	for range 4 {
		runSteeringHookForTest(t, repository, steeringTriggerCommand, &bytes.Buffer{})
	}
	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clean command state exists: %v", err)
	}
}

func TestSteeringStateStaysBoundedAfterAdvisory(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	for range 100 {
		runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	}
	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := nonemptyLines(contents); len(lines) > 3 {
		t.Fatalf("state has %d edit records after advisory, want at most 3", len(lines))
	}
}

func TestSteeringStateRecoversAfterTornWrite(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{partial"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if !strings.Contains(output.String(), "run atd test fast") {
		t.Fatalf("torn state lost prior edit intents: %q", output.String())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := nonemptyLines(contents)
	var event steeringEvent
	if err := json.Unmarshal(lines[len(lines)-1], &event); err != nil {
		t.Fatalf("last state record remains corrupt: %q: %v", lines[len(lines)-1], err)
	}
}

func TestSteeringAdvisoryBelongsToFirstThresholdEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	for _, event := range []steeringEvent{
		{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: "one"},
		{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: "two"},
		{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: "winner"},
		{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: "late"},
	} {
		if err := appendSteeringEvent(path, event); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadSteeringState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.editAdviser != "winner" {
		t.Fatalf("edit adviser = %q, want winner", state.editAdviser)
	}
	appendSteeringReset(path)
	if err := appendSteeringEvent(path, steeringEvent{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: "stale"}); err != nil {
		t.Fatal(err)
	}
	state, err = loadSteeringState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.editAdviser != "" || state.edits != 1 {
		t.Fatalf("state after reset = %#v, want one edit and no adviser", state)
	}
}

func TestSteeringFoldCountsConcurrentCleanIntentsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.jsonl")
	for _, id := range []string{"clean-one", "clean-two", "clean-three"} {
		if err := appendSteeringEvent(path, steeringEvent{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: id, Clean: true}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadSteeringState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.edits != 1 || state.editAdviser != "" {
		t.Fatalf("clean-intent state = %#v, want one edit and no adviser", state)
	}
	for _, id := range []string{"dirty-two", "dirty-winner"} {
		if err := appendSteeringEvent(path, steeringEvent{Record: steeringRecord, Trigger: steeringTriggerEdit, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	state, err = loadSteeringState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.edits != 3 || state.editAdviser != "dirty-winner" {
		t.Fatalf("dirty state = %#v, want dirty-winner at threshold", state)
	}
}

func TestSteeringTestResetIsScopedToItsWorktree(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var output bytes.Buffer
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "first\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)

	other := filepath.Join(t.TempDir(), "other")
	runTestRunnerGit(t, repository, "worktree", "add", "-q", "-b", "other", other)
	if err := runTestCommandWithMode(other, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	writeTestRunnerFile(t, filepath.Join(repository, "feature.go"), "second\n")
	runSteeringHookForTest(t, repository, steeringTriggerEdit, &output)
	if !strings.Contains(output.String(), "run atd test fast") {
		t.Fatalf("other worktree reset edit intents: %q", output.String())
	}
}

func TestSteeringHookFailsOpenOnOperationalError(t *testing.T) {
	repository := newTestRunnerRepository(t)
	installFakeGit(t, "#!/bin/sh\nexit 1\n")
	var output bytes.Buffer
	if err := runSteeringHook(repository, steeringTriggerEdit, &output); err != nil {
		t.Fatalf("operational hook error did not fail open: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("operational hook error emitted output: %q", output.String())
	}
}

func TestSteeringCLIHookFailsOpenOutsideGit(t *testing.T) {
	withTestRunnerDirectory(t, t.TempDir())
	if err := runCLI([]string{"hook", "edit"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("hook outside Git did not fail open: %v", err)
	}
}

func TestRunCLIDispatchesSteeringHookFromNestedDirectory(t *testing.T) {
	repository := newTestRunnerRepository(t)
	nested := filepath.Join(repository, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	withTestRunnerDirectory(t, nested)
	if err := runCLI([]string{"hook", "edit"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	path, err := steeringStatePath(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func runSteeringHookForTest(t *testing.T, repository string, trigger steeringTrigger, output *bytes.Buffer) {
	t.Helper()
	if err := runSteeringHook(repository, trigger, output); err != nil {
		t.Fatal(err)
	}
}

func nonemptyLines(contents []byte) [][]byte {
	var lines [][]byte
	for _, line := range bytes.Split(contents, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}
