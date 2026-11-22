package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const verifierScript = `#!/bin/sh
status=0
for spec in spec_*; do
	[ -e "$spec" ] || continue
	[ -f "$(cat "$spec")" ] || status=1
done
exit $status
`

type verifierFixture struct {
	repository string
	base       string
	candidate  string
	red        string
}

func newVerifiedFixture(t *testing.T) verifierFixture {
	t.Helper()
	repository := newVerifierRepository(t)
	base := verifierHead(t, repository)
	writeTestRunnerFile(t, filepath.Join(repository, "spec_feature"), "impl_feature")
	recordVerifierTest(t, repository, testModeFast, true)
	red := commitAll(t, repository, "test: add feature coverage")
	writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented REVIEW_SENTINEL")
	candidate := commitAll(t, repository, "feat: implement feature")
	recordVerifierTest(t, repository, testModeFast, false)
	recordVerifierTest(t, repository, testModeFull, false)
	return verifierFixture{repository: repository, base: base, candidate: candidate, red: red}
}

func TestVerifyReportsBoundRedAsAdvisoryAndRequiresExactCandidateGates(t *testing.T) {
	fixture := newVerifiedFixture(t)
	output := &bytes.Buffer{}
	if err := verifyCandidate(fixture.repository, fixture.base, fixture.candidate, output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"verification gates passed",
		"bound red candidates: 1",
		fixture.red,
		"command [\"./script/test_fast\"]",
		"candidate gates: fast green; full green",
		"outer-red evidence is advisory review input",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "REVIEW_SENTINEL") {
		t.Fatalf("verification duplicated reviewed diff:\n%s", output.String())
	}
}

func TestVerifyLeavesUnmatchedInnerRedsAdvisory(t *testing.T) {
	fixture := newVerifiedFixture(t)
	recordVerifierTest(t, fixture.repository, testModeTargeted, true)
	output := &bytes.Buffer{}
	if err := verifyCandidate(fixture.repository, fixture.base, fixture.candidate, output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "unbound credible reds: 1") {
		t.Fatalf("unmatched red was not summarized as advisory:\n%s", output.String())
	}
}

func TestVerifyBoundsRedCandidateOutput(t *testing.T) {
	bound := make([]boundRed, maxBoundRedSummary+1)
	for index := range bound {
		bound[index] = boundRed{Commit: strings.Repeat(string(rune('a'+index)), 40), Observation: testObservation{Command: []string{"test", string(rune('a' + index))}}}
	}
	bound[0].Observation.Command = []string{"test", strings.Repeat("x", maxCommandSummaryCharacters+50)}
	output := &bytes.Buffer{}
	printVerificationSummary(output, "", nil, nil, bound)
	if count := strings.Count(output.String(), "  bound red:"); count != maxBoundRedSummary {
		t.Fatalf("printed bound reds = %d, want %d:\n%s", count, maxBoundRedSummary, output.String())
	}
	if !strings.Contains(output.String(), "1 more bound red candidates") {
		t.Fatalf("summary did not report hidden candidates:\n%s", output.String())
	}
	if strings.Contains(output.String(), strings.Repeat("x", maxCommandSummaryCharacters+50)) || !strings.Contains(output.String(), "...") {
		t.Fatalf("summary did not truncate a long command:\n%s", output.String())
	}
}

func TestVerifyScopesRedSummaryToCurrentRange(t *testing.T) {
	first := newVerifiedFixture(t)
	base := first.candidate
	writeTestRunnerFile(t, filepath.Join(first.repository, "spec_second"), "impl_second")
	recordVerifierTest(t, first.repository, testModeFast, true)
	commitAll(t, first.repository, "test: add second feature coverage")
	writeTestRunnerFile(t, filepath.Join(first.repository, "impl_second"), "implemented")
	candidate := commitAll(t, first.repository, "feat: implement second feature")
	recordVerifierTest(t, first.repository, testModeFast, false)
	recordVerifierTest(t, first.repository, testModeFull, false)
	output := &bytes.Buffer{}
	if err := verifyCandidate(first.repository, base, candidate, output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"credible red observations: 1", "bound red candidates: 1", "unbound credible reds: 0"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("current-range summary missing %q:\n%s", want, output.String())
		}
	}
}

func TestVerifyLeavesMissingOuterRedForAdvisoryReview(t *testing.T) {
	repository := newVerifierRepository(t)
	base := verifierHead(t, repository)
	writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented")
	candidate := commitAll(t, repository, "feat: implement feature")
	recordVerifierTest(t, repository, testModeFast, false)
	recordVerifierTest(t, repository, testModeFull, false)
	output := &bytes.Buffer{}
	if err := verifyCandidate(repository, base, candidate, output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "bound red candidates: 0") {
		t.Fatalf("missing outer red was not surfaced for review:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "outer-red evidence is advisory review input") {
		t.Fatalf("missing outer red was presented as a gate:\n%s", output.String())
	}
}

func TestVerifyRequiresBothCandidateGates(t *testing.T) {
	for _, mode := range []testMode{testModeFast, testModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			repository := newVerifierRepository(t)
			base := verifierHead(t, repository)
			writeTestRunnerFile(t, filepath.Join(repository, "spec_feature"), "impl_feature")
			recordVerifierTest(t, repository, testModeFast, true)
			commitAll(t, repository, "test: add feature coverage")
			writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented")
			candidate := commitAll(t, repository, "feat: implement feature")
			recordVerifierTest(t, repository, mode, false)
			err := verifyCandidate(repository, base, candidate, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "green ") {
				t.Fatalf("verify error = %v", err)
			}
		})
	}
}

func TestVerifyAllowsStablePreCommitGreenForUnchangedCandidateTree(t *testing.T) {
	repository := newVerifierRepository(t)
	base := verifierHead(t, repository)
	writeTestRunnerFile(t, filepath.Join(repository, "spec_feature"), "impl_feature")
	recordVerifierTest(t, repository, testModeFast, true)
	red := commitAll(t, repository, "test: add feature coverage")
	writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented REVIEW_SENTINEL")
	recordVerifierTest(t, repository, testModeFast, false)
	recordVerifierTest(t, repository, testModeFull, false)
	candidate := commitAll(t, repository, "feat: implement feature")

	output := &bytes.Buffer{}
	if err := verifyCandidate(repository, base, candidate, output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"verification gates passed",
		"bound red candidates: 1",
		red,
		"candidate gates: fast green; full green",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestVerifyRejectsStaleCandidateEvidence(t *testing.T) {
	fixture := newVerifiedFixture(t)
	writeTestRunnerFile(t, filepath.Join(fixture.repository, "notes.md"), "later\n")
	candidate := commitAll(t, fixture.repository, "docs: update notes")
	err := verifyCandidate(fixture.repository, fixture.base, candidate, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "green fast") {
		t.Fatalf("verify error = %v", err)
	}
}

func TestVerifyUsesLatestCandidateGateObservation(t *testing.T) {
	for _, mode := range []testMode{testModeFast, testModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			fixture := newVerifiedFixture(t)
			script, otherMode := testFastScript, testModeFull
			if mode == testModeFull {
				script, otherMode = testFullScript, testModeFast
			}
			failure := latestTestObservation(t, fixture.repository)
			failure.Mode, failure.Command, failure.ExitStatus = mode, []string{"./script/" + script}, 1
			if _, err := appendTestObservation(fixture.repository, failure); err != nil {
				t.Fatal(err)
			}
			recordVerifierTest(t, fixture.repository, otherMode, false)
			err := verifyCandidate(fixture.repository, fixture.base, fixture.candidate, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "green "+string(mode)) {
				t.Fatalf("later %s failure was masked by a prior or other-gate green: %v", mode, err)
			}
			recordVerifierTest(t, fixture.repository, mode, false)
			if err := verifyCandidate(fixture.repository, fixture.base, fixture.candidate, &bytes.Buffer{}); err != nil {
				t.Fatalf("latest %s green did not restore acceptance: %v", mode, err)
			}
		})
	}
}

func TestCandidateGateMatchesIdentityBeforeJudgingLatestResult(t *testing.T) {
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherWorktree := t.TempDir()
	green := testObservation{
		Worktree: repository, BeforeTree: strings.Repeat("a", 40), AfterTree: strings.Repeat("a", 40),
		BeforeHead: strings.Repeat("b", 40), AfterHead: strings.Repeat("b", 40),
		Mode: testModeFast, Command: []string{"./script/test_fast"}, ObservedAt: "2026-09-08T12:00:00Z",
	}
	for _, test := range []struct {
		name   string
		change func(*testObservation)
		want   bool
	}{
		{"later red despite older clock", func(o *testObservation) { o.ExitStatus = 1 }, false},
		{"source changed", func(o *testObservation) { o.SourceChanged = true; o.AfterTree = strings.Repeat("c", 40) }, false},
		{"history changed", func(o *testObservation) { o.HistoryChanged = true; o.AfterHead = strings.Repeat("c", 40) }, false},
		{"signal", func(o *testObservation) { o.Signaled = true; o.ExitStatus = 137 }, false},
		{"launch failure", func(o *testObservation) { o.LaunchFailed = true; o.ExitStatus = 127 }, false},
		{"different starting tree", func(o *testObservation) { o.BeforeTree = strings.Repeat("c", 40); o.ExitStatus = 1 }, true},
		{"different worktree", func(o *testObservation) { o.Worktree = otherWorktree; o.ExitStatus = 1 }, true},
		{"different mode", func(o *testObservation) { o.Mode = testModeTargeted; o.ExitStatus = 1 }, true},
		{"different command", func(o *testObservation) { o.Command = []string{"false"}; o.ExitStatus = 1 }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			latest := green
			latest.ObservedAt = "2026-09-08T11:00:00Z"
			test.change(&latest)
			got := hasCandidateGreen([]testObservation{green, latest}, repository, green.BeforeTree, green.Mode, green.Command)
			if got != test.want {
				t.Fatalf("candidate green = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVerifyReportsExecutionAnomaliesAsAdvisory(t *testing.T) {
	fixture := newVerifiedFixture(t)
	observations, err := localTestObservations(fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*testObservation){
		func(o *testObservation) { o.SourceChanged = true; o.AfterTree = strings.Repeat("a", 40) },
		func(o *testObservation) { o.HistoryChanged = true; o.AfterHead = strings.Repeat("b", 40) },
		func(o *testObservation) { o.Signaled = true; o.ExitStatus = 137 },
		func(o *testObservation) { o.LaunchFailed = true; o.ExitStatus = 127 },
	} {
		observation := observations[len(observations)-1]
		observation.Mode, observation.Command, observation.ExitStatus = testModeTargeted, []string{"false"}, 1
		change(&observation)
		if _, err := appendTestObservation(fixture.repository, observation); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := verifyCandidate(fixture.repository, fixture.base, fixture.candidate, &output); err != nil {
		t.Fatalf("targeted anomalies rejected valid candidate gates: %v", err)
	}
	want := "run anomalies (advisory): source changes 1; history changes 1; signals 1; launch failures 1"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("missing anomaly counts:\n%s", output.String())
	}
}

func TestVerificationAnomalySummaryIsScopedAndBounded(t *testing.T) {
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherWorktree := t.TempDir()
	inScope := testObservation{
		Worktree: repository, BeforeHead: "included", SourceChanged: true, HistoryChanged: true,
		Signaled: true, LaunchFailed: true, Command: []string{strings.Repeat("DETAIL_SENTINEL", 100)},
	}
	for _, count := range []int{0, 200} {
		observations := make([]testObservation, count)
		for i := range observations {
			observations[i] = inScope
		}
		outsideRange, outsideWorktree := inScope, inScope
		outsideRange.BeforeHead = "excluded"
		outsideRange.AfterHead = "included"
		outsideWorktree.Worktree = otherWorktree
		observations = append(observations, outsideRange, outsideWorktree)
		var output bytes.Buffer
		printVerificationSummary(&output, repository, map[string]bool{"included": true}, observations, nil)
		want := fmt.Sprintf("run anomalies (advisory): source changes %d; history changes %d; signals %d; launch failures %d", count, count, count, count)
		if !strings.Contains(output.String(), want) || strings.Count(output.String(), "run anomalies") != 1 {
			t.Fatalf("incorrect or unbounded anomaly summary:\n%s", output.String())
		}
		if strings.Contains(output.String(), "DETAIL_SENTINEL") || len(output.String()) > 500 {
			t.Fatalf("summary expanded individual anomalies: %d bytes", len(output.String()))
		}
	}
}

func TestVerifyRequiresExactRegisteredGateCommand(t *testing.T) {
	repository := newVerifierRepository(t)
	base := verifierHead(t, repository)
	writeTestRunnerFile(t, filepath.Join(repository, "spec_feature"), "impl_feature")
	recordVerifierTest(t, repository, testModeFast, true)
	commitAll(t, repository, "test: add feature coverage")
	writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented")
	candidate := commitAll(t, repository, "feat: implement feature")
	if err := runTestCommandWithMode(repository, testModeFast, []string{"sh", "-c", "./script/test_fast"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	recordVerifierTest(t, repository, testModeFull, false)
	err := verifyCandidate(repository, base, candidate, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "green fast") {
		t.Fatalf("verify error = %v", err)
	}
}

func TestVerifyLocalEvidenceDoesNotSurviveFreshClone(t *testing.T) {
	fixture := newVerifiedFixture(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runTestRunnerGit(t, fixture.repository, "clone", "-q", fixture.repository, clone)
	recordVerifierTest(t, clone, testModeFast, false)
	recordVerifierTest(t, clone, testModeFull, false)
	output := &bytes.Buffer{}
	if err := verifyCandidate(clone, fixture.base, fixture.candidate, output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "bound red candidates: 0") {
		t.Fatalf("fresh clone claimed local red evidence:\n%s", output.String())
	}
}

func TestVerifyAllowsMergeInRange(t *testing.T) {
	fixture := newVerifiedFixture(t)
	originalBranch := strings.TrimSpace(runTestRunnerGit(t, fixture.repository, "branch", "--show-current"))
	runTestRunnerGit(t, fixture.repository, "checkout", "-q", "-b", "topic")
	writeTestRunnerFile(t, filepath.Join(fixture.repository, "topic.txt"), "topic\n")
	commitAll(t, fixture.repository, "docs: add topic note")
	runTestRunnerGit(t, fixture.repository, "checkout", "-q", originalBranch)
	writeTestRunnerFile(t, filepath.Join(fixture.repository, "main.txt"), "main\n")
	commitAll(t, fixture.repository, "docs: add main note")
	runTestRunnerGit(t, fixture.repository, "merge", "--no-ff", "-q", "topic", "-m", "merge topic")
	candidate := verifierHead(t, fixture.repository)
	recordVerifierTest(t, fixture.repository, testModeFast, false)
	recordVerifierTest(t, fixture.repository, testModeFull, false)
	if err := verifyCandidate(fixture.repository, fixture.base, candidate, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyScopesMergedBranchRedThatPredatesBase(t *testing.T) {
	repository := newVerifierRepository(t)
	forkPoint := verifierHead(t, repository)
	originalBranch := strings.TrimSpace(runTestRunnerGit(t, repository, "branch", "--show-current"))
	writeTestRunnerFile(t, filepath.Join(repository, "main.txt"), "main\n")
	base := commitAll(t, repository, "docs: advance main")
	runTestRunnerGit(t, repository, "checkout", "-q", "-b", "topic", forkPoint)
	writeTestRunnerFile(t, filepath.Join(repository, "spec_feature"), "impl_feature")
	recordVerifierTest(t, repository, testModeFast, true)
	commitAll(t, repository, "test: add feature coverage")
	writeTestRunnerFile(t, filepath.Join(repository, "impl_feature"), "implemented")
	commitAll(t, repository, "feat: implement feature")
	runTestRunnerGit(t, repository, "checkout", "-q", originalBranch)
	runTestRunnerGit(t, repository, "merge", "--no-ff", "-q", "topic", "-m", "merge topic")
	candidate := verifierHead(t, repository)
	recordVerifierTest(t, repository, testModeFast, false)
	recordVerifierTest(t, repository, testModeFull, false)
	output := &bytes.Buffer{}
	if err := verifyCandidate(repository, base, candidate, output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"credible red observations: 1", "bound red candidates: 1", "unbound credible reds: 0"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("merged-branch summary missing %q:\n%s", want, output.String())
		}
	}
}

func TestCLIVerifySucceedsAfterMechanicalGates(t *testing.T) {
	fixture := newVerifiedFixture(t)
	withTestRunnerDirectory(t, fixture.repository)
	var output bytes.Buffer
	if err := runCLI([]string{"verify", "--base", fixture.base, "--candidate", fixture.candidate}, &output); err != nil {
		t.Fatal(err)
	}
}

func TestParseVerifyCommand(t *testing.T) {
	base, candidate, err := parseVerifyCommand([]string{"--candidate", "two", "--base", "one"})
	if err != nil || base != "one" || candidate != "two" {
		t.Fatalf("parse = %q, %q, %v", base, candidate, err)
	}
	if _, _, err := parseVerifyCommand([]string{"--base", "one", "--base", "two"}); err == nil {
		t.Fatal("accepted repeated base flag")
	}
}

func newVerifierRepository(t *testing.T) string {
	t.Helper()
	repository := newTestRunnerRepository(t)
	if err := initializeTestScripts(repository); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{testFastScript, testFullScript} {
		writeTestRunnerFile(t, filepath.Join(repository, "script", name), verifierScript)
	}
	commitAll(t, repository, "chore: register test scripts")
	return repository
}

func recordVerifierTest(t *testing.T, repository string, mode testMode, wantRed bool) {
	t.Helper()
	command := []string{"./script/" + testFastScript}
	if mode == testModeFull {
		command = []string{"./script/" + testFullScript}
	}
	if mode == testModeTargeted {
		command = []string{"false"}
	}
	err := runTestCommandWithMode(repository, mode, command, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if wantRed {
		var child testCommandExitError
		if !errors.As(err, &child) {
			t.Fatalf("runner error = %v, want red", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, repository, subject string) string {
	t.Helper()
	runTestRunnerGit(t, repository, "add", "-A")
	runTestRunnerGit(t, repository, "commit", "-q", "-m", subject)
	return verifierHead(t, repository)
}

func verifierHead(t *testing.T, repository string) string {
	t.Helper()
	return strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", "HEAD"))
}
