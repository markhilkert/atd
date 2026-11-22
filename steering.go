package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const steeringRecord = "atd-steering/v1"

type steeringTrigger string

const (
	steeringTriggerEdit    steeringTrigger = "edit"
	steeringTriggerCommand steeringTrigger = "command"
	steeringTriggerReset   steeringTrigger = "reset"
)

type steeringEvent struct {
	Record  string          `json:"record"`
	Trigger steeringTrigger `json:"trigger"`
	ID      string          `json:"id,omitempty"`
	Clean   bool            `json:"clean,omitempty"`
}

type steeringState struct {
	edits, commands             int
	editAdviser, commandAdviser string
	cleanIntent                 bool
}

func runSteeringHook(repository string, trigger steeringTrigger, output io.Writer) error {
	if trigger != steeringTriggerEdit && trigger != steeringTriggerCommand {
		return fmt.Errorf("unknown hook trigger %q", trigger)
	}
	path, err := steeringStatePath(repository)
	if err != nil {
		return nil
	}
	state, err := loadSteeringState(path)
	if err != nil {
		return nil
	}
	// Past its threshold a trigger records and prints nothing until an observed
	// test resets it, so skip the working-tree scan.
	if trigger == steeringTriggerEdit && state.edits >= 3 || trigger == steeringTriggerCommand && state.commands >= 3 {
		return nil
	}
	dirty, source, tests, err := steeringChanges(repository)
	if err != nil {
		return nil
	}
	if trigger == steeringTriggerCommand && !source && !tests {
		return nil
	}
	if trigger == steeringTriggerEdit && dirty && !source && !tests {
		return nil
	}
	if trigger == steeringTriggerEdit && !dirty && state.edits > 0 {
		return nil
	}
	id, err := stateEventID()
	if err != nil {
		return nil
	}
	if appendSteeringEvent(path, steeringEvent{Record: steeringRecord, Trigger: trigger, ID: id, Clean: trigger == steeringTriggerEdit && !dirty}) != nil {
		return nil
	}
	state, err = loadSteeringState(path)
	if err != nil {
		return nil
	}
	if trigger == steeringTriggerEdit && state.editAdviser == id {
		if source {
			_, _ = fmt.Fprintln(output, "atd: source edits are accumulating without an observed test; run atd test fast")
		} else if tests {
			_, _ = fmt.Fprintln(output, "atd: run the changed test with atd test fast or atd test -- <command> and confirm the intended red")
		}
	}
	if trigger == steeringTriggerCommand && state.commandAdviser == id {
		_, _ = fmt.Fprintln(output, "atd: if this command runs tests, use atd test fast, full, or -- <command> so the result is observed")
	}
	return nil
}

func stateEventID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func steeringStatePath(repository string) (string, error) {
	return testObservationIndexPath(repository)
}

func appendSteeringReset(path string) {
	_ = appendSteeringEvent(path, steeringEvent{Record: steeringRecord, Trigger: steeringTriggerReset})
}

func loadSteeringState(path string) (steeringState, error) {
	contents, err := boundedJSONLTail(path)
	if os.IsNotExist(err) {
		return steeringState{}, nil
	}
	if err != nil {
		return steeringState{}, err
	}
	var state steeringState
	for _, line := range bytes.Split(contents, []byte{'\n'}) {
		var event steeringEvent
		if json.Unmarshal(line, &event) != nil || event.Record != steeringRecord {
			continue
		}
		switch event.Trigger {
		case steeringTriggerReset:
			state = steeringState{}
		case steeringTriggerEdit:
			if event.Clean && state.cleanIntent {
				continue
			}
			state.cleanIntent = state.cleanIntent || event.Clean
			state.edits++
			if state.edits == 3 {
				state.editAdviser = event.ID
			}
		case steeringTriggerCommand:
			state.commands++
			if state.commands == 3 {
				state.commandAdviser = event.ID
			}
		}
	}
	return state, nil
}

func appendSteeringEvent(path string, event steeringEvent) error {
	contents, err := json.Marshal(event)
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	return appendJSONL(path, contents)
}

func steeringChanges(repository string) (bool, bool, bool, error) {
	paths, err := steeringChangedPaths(repository)
	if err != nil || len(paths) == 0 {
		return false, false, false, err
	}
	var source, tests bool
	for _, path := range paths {
		switch {
		case steeringTestPath(path):
			tests = true
		case !steeringDocumentationPath(path):
			source = true
		}
	}
	return true, source, tests, nil
}

// steeringChangedPaths lists staged, unstaged and untracked paths with one
// scan. In -z output a rename or copy entry is followed by its original path,
// which is skipped so the change classifies by its new path.
func steeringChangedPaths(repository string) ([]string, error) {
	output, err := gitOutput(repository, nil, nil, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	entries := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	var paths []string
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		if strings.ContainsAny(entry[:2], "RC") {
			i++
		}
	}
	return paths, nil
}

func steeringTestPath(path string) bool {
	path = filepath.ToSlash(path)
	base, lower := filepath.Base(path), strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(path, "tests/") || strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "spec/") || strings.HasPrefix(path, "__tests__/") || strings.Contains(path, "/tests/") || strings.Contains(path, "/test/") || strings.Contains(path, "/spec/") || strings.Contains(path, "/__tests__/") || strings.HasPrefix(lower, "test_") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.Contains(base, "_test.") || strings.Contains(base, "_spec.") || strings.HasSuffix(base, "_test.go") || strings.HasSuffix(lower, "test.java") || path == "script/test" || path == "script/test_fast"
}

func steeringDocumentationPath(path string) bool {
	path = filepath.ToSlash(path)
	base := filepath.Base(path)
	return strings.HasPrefix(path, "docs/") || strings.HasPrefix(path, "doc/") || strings.HasPrefix(strings.ToLower(base), "readme") || strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".mdx") || strings.HasSuffix(base, ".rst") || strings.HasSuffix(base, ".adoc")
}
