package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	testObservationSchema = "atd-test-observation/v3"
	testObservationRef    = "refs/atd/test-observations"
)

type testMode string

const (
	testModeFast     testMode = "fast"
	testModeFull     testMode = "full"
	testModeTargeted testMode = "targeted"
)

type testObservation struct {
	Schema         string   `json:"schema"`
	Worktree       string   `json:"worktree"`
	BeforeTree     string   `json:"before_tree"`
	AfterTree      string   `json:"after_tree"`
	SourceChanged  bool     `json:"source_changed"`
	BeforeHead     string   `json:"before_head"`
	AfterHead      string   `json:"after_head"`
	HistoryChanged bool     `json:"history_changed"`
	Signaled       bool     `json:"signaled"`
	LaunchFailed   bool     `json:"launch_failed"`
	Mode           testMode `json:"mode"`
	Command        []string `json:"command"`
	ExitStatus     int      `json:"exit_status"`
	ObservedAt     string   `json:"observed_at"`
}

func appendTestObservation(repository string, observation testObservation) (bool, error) {
	contents, err := json.Marshal(observation)
	if err != nil {
		return false, err
	}
	contents = append(contents, '\n')
	blob, err := gitOutput(repository, nil, contents, "hash-object", "-w", "--stdin")
	if err != nil {
		return false, err
	}
	treeInput := fmt.Sprintf("100644 blob %s\tobservation.json\n", strings.TrimSpace(blob))
	tree, err := gitOutput(repository, nil, []byte(treeInput), "mktree")
	if err != nil {
		return false, err
	}
	commitEnvironment := gitEnvironment(map[string]string{
		"GIT_AUTHOR_EMAIL":    "atd@localhost",
		"GIT_AUTHOR_NAME":     "atd",
		"GIT_COMMITTER_EMAIL": "atd@localhost",
		"GIT_COMMITTER_NAME":  "atd",
	})
	lockDeadline := time.Now().Add(5 * time.Second)
	for delay := 10 * time.Millisecond; ; delay = min(delay*2, 250*time.Millisecond) {
		parent, exists, err := testObservationHead(repository)
		if err != nil {
			return false, err
		}
		previous, found, err := previousTestObservation(repository, parent, observation.Worktree, observation.Mode, observation.Command)
		if err != nil {
			return false, err
		}
		arguments := []string{"commit-tree", strings.TrimSpace(tree)}
		if exists {
			arguments = append(arguments, "-p", parent)
		}
		commit, err := gitOutput(repository, commitEnvironment, []byte("atd test observation\n"), arguments...)
		if err != nil {
			return false, err
		}
		expected := parent
		if !exists {
			expected = strings.Repeat("0", len(strings.TrimSpace(commit)))
		}
		if _, err := gitOutput(repository, nil, nil, "update-ref", testObservationRef, strings.TrimSpace(commit), expected); err == nil {
			appendTestObservationIndex(repository, parent, strings.TrimSpace(commit), observation)
			return found && credibleRed(previous) && observation.ExitStatus == 0, nil
		} else if refUpdateCASContention(err) {
			time.Sleep(delay)
			continue
		} else if !refUpdateLockContention(err) || time.Now().After(lockDeadline) {
			return false, err
		}
		time.Sleep(delay)
	}
}

func refUpdateCASContention(err error) bool {
	message := err.Error()
	return strings.Contains(message, "cannot lock ref") && (strings.Contains(message, "is at ") || strings.Contains(message, "reference already exists"))
}

func refUpdateLockContention(err error) bool {
	return strings.Contains(err.Error(), "cannot lock ref") && strings.Contains(err.Error(), "File exists")
}

func testObservationHead(repository string) (string, bool, error) {
	output, err := gitOutput(repository, nil, nil, "rev-parse", "--verify", "--quiet", testObservationRef)
	if err == nil {
		return strings.TrimSpace(output), true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, err
}

func validateTestObservation(observation testObservation) error {
	if observation.Worktree == "" {
		return errors.New("worktree must be nonempty")
	}
	if observation.Schema != testObservationSchema {
		return fmt.Errorf("uses unsupported schema %q", observation.Schema)
	}
	if !validVerifierObjectID(observation.BeforeTree) {
		return errors.New("before_tree must be a lowercase 40- or 64-character hexadecimal object ID")
	}
	if !validVerifierObjectID(observation.AfterTree) {
		return errors.New("after_tree must be a lowercase 40- or 64-character hexadecimal object ID")
	}
	if !validVerifierObjectID(observation.BeforeHead) {
		return errors.New("before_head must be a lowercase 40- or 64-character hexadecimal object ID")
	}
	if !validVerifierObjectID(observation.AfterHead) {
		return errors.New("after_head must be a lowercase 40- or 64-character hexadecimal object ID")
	}
	if observation.SourceChanged != (observation.BeforeTree != observation.AfterTree) {
		return errors.New("source_changed must match before_tree and after_tree")
	}
	if observation.HistoryChanged != (observation.BeforeHead != observation.AfterHead) {
		return errors.New("history_changed must match before_head and after_head")
	}
	if observation.Mode != testModeFast && observation.Mode != testModeFull && observation.Mode != testModeTargeted {
		return errors.New("mode must be fast, full, or targeted")
	}
	if len(observation.Command) == 0 {
		return errors.New("command must be nonempty")
	}
	if observation.ExitStatus < 0 || observation.ExitStatus > 255 {
		return errors.New("exit_status must be between 0 and 255")
	}
	if observation.Signaled && observation.LaunchFailed {
		return errors.New("signaled and launch_failed cannot both be true")
	}
	if observation.Signaled && (observation.ExitStatus < 129 || observation.ExitStatus > 192) {
		return errors.New("signaled requires exit_status between 129 and 192")
	}
	if observation.LaunchFailed && observation.ExitStatus != 126 && observation.ExitStatus != 127 {
		return errors.New("launch_failed requires exit_status 126 or 127")
	}
	if _, err := time.Parse(time.RFC3339Nano, observation.ObservedAt); err != nil {
		return errors.New("observed_at must be an RFC3339 timestamp")
	}
	return nil
}

func validVerifierObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func stableObservation(observation testObservation) bool {
	return !observation.SourceChanged && !observation.HistoryChanged && observation.BeforeTree == observation.AfterTree && observation.BeforeHead == observation.AfterHead
}

func credibleRed(observation testObservation) bool {
	return (observation.Mode == testModeFast || observation.Mode == testModeTargeted) && observation.ExitStatus != 0 && !observation.LaunchFailed && !observation.Signaled && stableObservation(observation)
}

func localTestObservations(repository string) ([]testObservation, error) {
	head, exists, err := testObservationHead(repository)
	if err != nil {
		return nil, fmt.Errorf("reading local observations: %w", err)
	}
	if !exists {
		return nil, nil
	}
	commits, err := gitOutput(repository, nil, nil, "rev-list", "--reverse", head)
	if err != nil {
		return nil, fmt.Errorf("reading local observations: %w", err)
	}
	observations := make([]testObservation, 0, len(strings.Fields(commits)))
	for _, commit := range strings.Fields(commits) {
		contents, err := gitOutput(repository, nil, nil, "show", commit+":observation.json")
		if err != nil {
			return nil, fmt.Errorf("reading local observation %s: %w", commit, err)
		}
		var observation testObservation
		if err := json.Unmarshal([]byte(contents), &observation); err != nil {
			return nil, fmt.Errorf("decoding local observation %s: %w", commit, err)
		}
		if err := validateTestObservation(observation); err != nil {
			return nil, fmt.Errorf("local observation %s %w", commit, err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}
