package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunTestCommandStreamsOutputPreservesStatusAndRecordsObservation(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runTestCommandWithMode(repository, testModeTargeted, []string{"sh", "-c", "printf stdout; printf stderr >&2; exit 7"}, strings.NewReader(""), &stdout, &stderr)
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 7 {
		t.Fatalf("error = %v", err)
	}
	if stdout.String() != "stdout\natd: test is red; if this is the intended failure, commit the test before continuing\n" || stderr.String() != "stderr" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}

	observation := latestTestObservation(t, repository)
	if observation.ExitStatus != 7 {
		t.Fatalf("exit status = %d", observation.ExitStatus)
	}
	if observation.Mode != testModeTargeted {
		t.Fatalf("mode = %q, want %q", observation.Mode, testModeTargeted)
	}
	if strings.Join(observation.Command, "\x00") != "sh\x00-c\x00printf stdout; printf stderr >&2; exit 7" {
		t.Fatalf("command = %#v", observation.Command)
	}
	if observation.BeforeTree == "" || observation.AfterTree == "" {
		t.Fatal("observation has no before/after tree")
	}
	if observation.BeforeHead == "" || observation.AfterHead == "" || observation.HistoryChanged {
		t.Fatalf("observation history = %#v", observation)
	}
}

func TestRunTestCommandAdvisesOnStableRedFailure(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var stdout bytes.Buffer
	err := runTestCommandWithMode(repository, testModeTargeted, []string{"sh", "-c", "printf child; exit 7"}, strings.NewReader(""), &stdout, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 7 {
		t.Fatalf("error = %v", err)
	}
	want := "child\natd: test is red; if this is the intended failure, commit the test before continuing\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunTestCommandDoesNotAdviseOnExcludedFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		command func(string) []string
	}{
		{name: "permission", command: func(repository string) []string {
			path := filepath.Join(repository, "not-executable")
			writeTestRunnerFile(t, path, "#!/bin/sh\n")
			return []string{path}
		}},
		{name: "not found", command: func(repository string) []string {
			return []string{filepath.Join(repository, "missing")}
		}},
		{name: "signal", command: func(string) []string { return []string{"sh", "-c", "kill -TERM $$"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			var stdout bytes.Buffer
			if err := runTestCommandWithMode(repository, testModeTargeted, test.command(repository), strings.NewReader(""), &stdout, &bytes.Buffer{}); err == nil {
				t.Fatal("command returned no error")
			}
			if strings.Contains(stdout.String(), "test is red") {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestRunTestCommandPersistsSignalState(t *testing.T) {
	repository := newTestRunnerRepository(t)
	err := runTestCommandWithMode(repository, testModeTargeted, []string{"sh", "-c", "kill -TERM $$"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 143 {
		t.Fatalf("error = %v", err)
	}
	observation := latestTestObservation(t, repository)
	if !observation.Signaled || observation.ExitStatus != 143 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestRunTestCommandDoesNotNudgeAfterSignaledFailure(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "test -f green || kill -TERM $$"}
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("signaled command returned no error")
	}
	writeTestRunnerFile(t, filepath.Join(repository, "green"), "")
	var output bytes.Buffer
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("signaled failure issued green nudge: %q", output.String())
	}
}

func TestRunTestCommandDoesNotNudgeAfterLaunchFailure(t *testing.T) {
	repository := newTestRunnerRepository(t)
	commandPath := filepath.Join(repository, "eventual-command")
	command := []string{commandPath}
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing command returned no error")
	}
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("launch failure issued green nudge: %q", output.String())
	}
}

func TestRunTestCommandMapsInvalidExecutableLaunchFailureTo126(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := filepath.Join(repository, "invalid-executable")
	if err := os.WriteFile(command, []byte("not an executable format\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := runTestCommandWithMode(repository, testModeTargeted, []string{command}, strings.NewReader(""), &stdout, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 126 {
		t.Fatalf("error = %v, want launch status 126", err)
	}
	observation := latestTestObservation(t, repository)
	if observation.ExitStatus != 126 || !observation.LaunchFailed || credibleRed(observation) {
		t.Fatalf("observation = %#v, want excluded launch failure", observation)
	}
	if strings.Contains(stdout.String(), "test is red") {
		t.Fatalf("launch failure emitted red evidence: %q", stdout.String())
	}
}

func TestRunTestCommandMapsMissingInterpreterLaunchFailureTo127(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := filepath.Join(repository, "missing-interpreter")
	if err := os.WriteFile(command, []byte("#!/no/such/interpreter\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := runTestCommandWithMode(repository, testModeTargeted, []string{command}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 127 {
		t.Fatalf("error = %v, want launch status 127", err)
	}
	observation := latestTestObservation(t, repository)
	if observation.ExitStatus != 127 || !observation.LaunchFailed || credibleRed(observation) {
		t.Fatalf("observation = %#v, want excluded not-found launch failure", observation)
	}
}

func TestObservedTreeMatchesDirtyWorktreeWhenItIsCommitted(t *testing.T) {
	repository := newTestRunnerRepository(t)
	writeTestRunnerFile(t, filepath.Join(repository, "tracked.txt"), "changed\n")
	writeTestRunnerFile(t, filepath.Join(repository, "untracked.txt"), "new\n")

	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	observedTree := latestTestObservation(t, repository).AfterTree
	runTestRunnerGit(t, repository, "add", "-A")
	runTestRunnerGit(t, repository, "commit", "-m", "test: capture observed tree")
	committedTree := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", "HEAD^{tree}"))
	if observedTree != committedTree {
		t.Fatalf("observed tree = %s, committed tree = %s", observedTree, committedTree)
	}
}

func TestObservationsAppendToGitHistory(t *testing.T) {
	repository := newTestRunnerRepository(t)
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"false"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("false returned no error")
	}
	first := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	parent := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef+"^"))
	if parent != first {
		t.Fatalf("second observation parent = %s, want %s", parent, first)
	}
}

func TestRunTestCommandNudgesWhenSameCommandTurnsGreen(t *testing.T) {
	for _, mode := range []testMode{testModeTargeted, testModeFast} {
		t.Run(string(mode), func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			marker := filepath.Join(repository, "green")
			command := []string{"sh", "-c", "printf child; test -f green"}
			if mode == testModeFast {
				if err := os.Mkdir(filepath.Join(repository, "script"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repository, "script", "test_fast"), []byte("#!/bin/sh\nprintf child\ntest -f green\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				command = []string{"./script/test_fast"}
			}

			if err := runTestCommandWithMode(repository, mode, command, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
				t.Fatal("red command returned no error")
			}
			writeTestRunnerFile(t, marker, "")
			var stdout bytes.Buffer
			if err := runTestCommandWithMode(repository, mode, command, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "child\natd: test is green after red; commit, then refactor with tests green before the next test\n" {
				t.Fatalf("stdout = %q", stdout.String())
			}
			stdout.Reset()
			if err := runTestCommandWithMode(repository, mode, command, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "child" {
				t.Fatalf("repeated green stdout = %q", stdout.String())
			}
		})
	}
}

func TestRunTestCommandDoesNotAddBlankLineBeforeNudge(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "test -f green || exit 1; printf 'child\\n'"}
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("red command returned no error")
	}
	writeTestRunnerFile(t, filepath.Join(repository, "green"), "")
	var stdout bytes.Buffer
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "child\natd: test is green after red; commit, then refactor with tests green before the next test\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunTestCommandDoesNotNudgeFirstOrRepeatedGreenOrDifferentCommand(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var stdout bytes.Buffer
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "" {
		t.Fatalf("first green nudge = %q", stdout.String())
	}
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"false"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("red command returned no error")
	}
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "" {
		t.Fatalf("different command nudge = %q", stdout.String())
	}
}

func TestRunTestCommandNudgeRequiresSameWorktree(t *testing.T) {
	repository := newTestRunnerRepository(t)
	_, err := appendTestObservation(repository, testObservation{
		Schema:     testObservationSchema,
		Worktree:   filepath.Join(repository, "another-worktree"),
		AfterTree:  "irrelevant",
		Command:    []string{"true"},
		ExitStatus: 1,
		ObservedAt: "2026-08-29T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "" {
		t.Fatalf("other worktree nudge = %q", stdout.String())
	}
}

func TestRunTestCommandFailsClosedAndDoesNotNudgeWhenGreenCommandChangesSource(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "test -f trigger || exit 1; touch source"}
	if err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("red command returned no error")
	}
	writeTestRunnerFile(t, filepath.Join(repository, "trigger"), "")
	var stdout bytes.Buffer
	err := runTestCommandWithMode(repository, testModeTargeted, command, strings.NewReader(""), &stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "changed source") {
		t.Fatalf("error = %v, want source-change failure", err)
	}
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 1 {
		t.Fatalf("error = %v, want status 1", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("source-changing command nudged: %q", stdout.String())
	}
	observation := latestTestObservation(t, repository)
	if !observation.SourceChanged || observation.BeforeTree == observation.AfterTree {
		t.Fatalf("observation = %#v, want source change", observation)
	}
}

func TestRunTestCommandPreservesNonzeroStatusWhenCommandChangesSource(t *testing.T) {
	repository := newTestRunnerRepository(t)
	var stdout bytes.Buffer
	err := runTestCommandWithMode(repository, testModeTargeted, []string{"sh", "-c", "touch changed; exit 7"}, strings.NewReader(""), &stdout, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 7 {
		t.Fatalf("error = %v, want status 7", err)
	}
	if !strings.Contains(err.Error(), "changed source") {
		t.Fatalf("error = %v, want source-change problem", err)
	}
	if strings.Contains(stdout.String(), "test is red") {
		t.Fatalf("source-changing command received red advisory: %q", stdout.String())
	}
	if !latestTestObservation(t, repository).SourceChanged {
		t.Fatal("observation did not persist source change")
	}
}

func TestRunTestCommandFailsClosedWhenCommandMovesHEAD(t *testing.T) {
	repository := newTestRunnerRepository(t)
	err := runTestCommandWithMode(repository, testModeTargeted, []string{"git", "commit", "--allow-empty", "-m", "test: move head"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	var child interface{ ExitStatus() int }
	if !errors.As(err, &child) || child.ExitStatus() != 1 || !strings.Contains(err.Error(), "changed history") {
		t.Fatalf("error = %v", err)
	}
	observation := latestTestObservation(t, repository)
	if !observation.HistoryChanged || observation.BeforeHead == observation.AfterHead {
		t.Fatalf("observation = %#v, want history change", observation)
	}
}

func TestRunTestCommandPersistsStableBeforeAndAfterTrees(t *testing.T) {
	repository := newTestRunnerRepository(t)
	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	observation := latestTestObservation(t, repository)
	if observation.BeforeTree != observation.AfterTree || observation.SourceChanged {
		t.Fatalf("observation = %#v, want stable trees", observation)
	}
}

func TestRunTestCommandUsesShellStatusesForLaunchFailures(t *testing.T) {
	repository := newTestRunnerRepository(t)
	notExecutable := filepath.Join(repository, "not-executable")
	writeTestRunnerFile(t, notExecutable, "#!/bin/sh\n")
	tests := []struct {
		name    string
		command string
		status  int
	}{
		{name: "permission denied", command: notExecutable, status: 126},
		{name: "not found", command: filepath.Join(repository, "missing"), status: 127},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runTestCommandWithMode(repository, testModeTargeted, []string{test.command}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
			var child interface{ ExitStatus() int }
			if !errors.As(err, &child) || child.ExitStatus() != test.status {
				t.Fatalf("error = %v", err)
			}
			if observation := latestTestObservation(t, repository); observation.ExitStatus != test.status {
				t.Fatalf("recorded status = %d", observation.ExitStatus)
			}
			if observation := latestTestObservation(t, repository); !observation.LaunchFailed {
				t.Fatalf("observation = %#v, want launch failure", observation)
			}
		})
	}
}

func TestExecutedChildStatuses126And127AreCredibleRed(t *testing.T) {
	for _, status := range []int{126, 127} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			var stdout bytes.Buffer
			err := runTestCommandWithMode(repository, testModeTargeted, []string{"sh", "-c", "exit " + strconv.Itoa(status)}, strings.NewReader(""), &stdout, &bytes.Buffer{})
			var child interface{ ExitStatus() int }
			if !errors.As(err, &child) || child.ExitStatus() != status {
				t.Fatalf("error = %v", err)
			}
			observation := latestTestObservation(t, repository)
			if observation.LaunchFailed || !credibleRed(observation) {
				t.Fatalf("observation = %#v, want credible child red", observation)
			}
			if !strings.Contains(stdout.String(), "test is red") {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestConcurrentTestObservationsAreNotLost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contention test in short mode")
	}
	repository := newTestRunnerRepository(t)
	const observations = 32
	start := make(chan struct{})
	errorsByRun := make(chan error, observations)
	var runners sync.WaitGroup
	for range observations {
		runners.Add(1)
		go func() {
			defer runners.Done()
			<-start
			errorsByRun <- runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
		}()
	}
	close(start)
	runners.Wait()
	close(errorsByRun)
	for err := range errorsByRun {
		if err != nil {
			t.Fatal(err)
		}
	}
	count, err := strconv.Atoi(strings.TrimSpace(runTestRunnerGit(t, repository, "rev-list", "--count", testObservationRef)))
	if err != nil {
		t.Fatal(err)
	}
	if count != observations {
		t.Fatalf("observation count = %d, want %d", count, observations)
	}
}

func TestAppendTestObservationOutlastsTransientRefContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contention test in short mode")
	}
	repository := newTestRunnerRepository(t)
	refPath := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", "--git-path", testObservationRef))
	if !filepath.IsAbs(refPath) {
		refPath = filepath.Join(repository, refPath)
	}
	if err := os.MkdirAll(filepath.Dir(refPath), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := refPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(2 * time.Second)
		_ = os.Remove(lockPath)
		close(released)
	}()
	t.Cleanup(func() {
		<-released
		_ = os.Remove(lockPath)
	})

	observation := testObservation{
		Schema:     testObservationSchema,
		Worktree:   repository,
		Mode:       testModeTargeted,
		Command:    []string{"true"},
		ExitStatus: 0,
		ObservedAt: "2026-08-29T00:00:00Z",
	}
	if _, err := appendTestObservation(repository, observation); err != nil {
		t.Fatalf("append during transient contention: %v", err)
	}
	count := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-list", "--count", testObservationRef))
	if count != "1" {
		t.Fatalf("observation count = %s, want 1", count)
	}
}

func TestWorkingTreeIdentityDoesNotRetainDirtySourceBlob(t *testing.T) {
	repository := newTestRunnerRepository(t)
	contents := "uncommitted source bytes unique to this test\n"
	path := filepath.Join(repository, "uncommitted.txt")
	writeTestRunnerFile(t, path, contents)
	blob := strings.TrimSpace(runTestRunnerGitWithInput(t, repository, contents, "hash-object", "--stdin"))

	if err := runTestCommandWithMode(repository, testModeTargeted, []string{"true"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", repository, "cat-file", "-e", blob)
	if err := command.Run(); err == nil {
		t.Fatalf("dirty source blob %s was retained", blob)
	}
}

func TestGitOutputReturnsOnlyStdoutWhenGitWarns(t *testing.T) {
	repository := t.TempDir()
	installFakeGit(t, "#!/bin/sh\nprintf 'deadbeef\\n'\nprintf 'warning from git\\n' >&2\n")

	output, err := gitOutput(repository, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if output != "deadbeef\n" {
		t.Fatalf("output = %q, want %q", output, "deadbeef\n")
	}
}

func TestGitOutputRetainsStdoutAndStderrOnError(t *testing.T) {
	repository := t.TempDir()
	installFakeGit(t, "#!/bin/sh\nprintf 'stdout diagnostic\\n'\nprintf 'stderr diagnostic\\n' >&2\nexit 1\n")

	output, err := gitOutput(repository, nil, nil, "rev-parse", "HEAD")
	if err == nil {
		t.Fatal("gitOutput returned no error")
	}
	if output != "" {
		t.Fatalf("output = %q, want empty output", output)
	}
	for _, diagnostic := range []string{"stdout diagnostic", "stderr diagnostic"} {
		if !strings.Contains(err.Error(), diagnostic) {
			t.Fatalf("error = %q, missing %q", err, diagnostic)
		}
	}
}

func newTestRunnerRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runTestRunnerGit(t, repository, "init", "-q")
	runTestRunnerGit(t, repository, "config", "user.name", "Test User")
	runTestRunnerGit(t, repository, "config", "user.email", "test@example.com")
	writeTestRunnerFile(t, filepath.Join(repository, "tracked.txt"), "initial\n")
	runTestRunnerGit(t, repository, "add", "tracked.txt")
	runTestRunnerGit(t, repository, "commit", "-q", "-m", "test: initialize repository")
	return repository
}

func latestTestObservation(t *testing.T, repository string) testObservation {
	t.Helper()
	contents := runTestRunnerGit(t, repository, "show", testObservationRef+":observation.json")
	var observation testObservation
	if err := json.Unmarshal([]byte(contents), &observation); err != nil {
		t.Fatal(err)
	}
	return observation
}

func writeTestRunnerFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installFakeGit(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runTestRunnerGit(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func runTestRunnerGitWithInput(t *testing.T, repository, input string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
