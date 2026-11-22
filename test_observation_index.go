package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	testObservationIndexPathname = "atd/test-observation-index.jsonl"
	indexRecord                  = "atd-observation-index/v1"
)

type testObservationIndex map[string]testObservation

type testObservationIndexEvent struct {
	Record      string           `json:"record"`
	Kind        string           `json:"kind"`
	Generation  string           `json:"generation"`
	Head        string           `json:"head,omitempty"`
	Key         string           `json:"key,omitempty"`
	Observation *testObservation `json:"observation,omitempty"`
}

func previousTestObservation(repository, head, worktree string, mode testMode, command []string) (testObservation, bool, error) {
	if head == "" {
		return testObservation{}, false, nil
	}
	index, current, err := loadTestObservationIndex(repository, head)
	if err != nil {
		return testObservation{}, false, err
	}
	if !current {
		index, err = rebuildTestObservationIndex(repository, head)
		if err != nil {
			return testObservation{}, false, err
		}
	}
	observation, found := index[testObservationIndexKey(worktree, mode, command)]
	return observation, found, nil
}

func appendTestObservationIndex(repository, previousHead, head string, observation testObservation) {
	path, err := testObservationIndexPath(repository)
	if err != nil {
		return
	}
	defer appendSteeringReset(path)
	if previousHead == "" {
		index := make(testObservationIndex)
		index[testObservationIndexKey(observation.Worktree, observation.Mode, observation.Command)] = observation
		writeTestObservationIndexGeneration(path, head, index)
		return
	}
	marker, found := loadLatestTestObservationIndexMarker(path)
	if !found || marker.Head != previousHead {
		return
	}
	key := testObservationIndexKey(observation.Worktree, observation.Mode, observation.Command)
	_ = appendTestObservationIndexEvents(path, []testObservationIndexEvent{
		{Record: indexRecord, Kind: "observation", Generation: marker.Generation, Key: key, Observation: &observation},
		{Record: indexRecord, Kind: "head", Generation: marker.Generation, Head: head},
	})
}

func loadLatestTestObservationIndexMarker(path string) (testObservationIndexEvent, bool) {
	file, err := os.Open(path)
	if err != nil {
		return testObservationIndexEvent{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return testObservationIndexEvent{}, false
	}
	var marker testObservationIndexEvent
	found := visitReverseJSONLFileLines(file, info.Size(), func(line []byte) bool {
		if json.Unmarshal(line, &marker) != nil || marker.Record != indexRecord || marker.Kind != "head" || marker.Generation == "" || marker.Head == "" {
			return false
		}
		return true
	})
	return marker, found
}

func loadTestObservationIndex(repository, head string) (testObservationIndex, bool, error) {
	path, err := testObservationIndexPath(repository)
	if err != nil {
		return nil, false, err
	}
	index, current := loadTestObservationIndexFile(path, head)
	return index, current, nil
}

func loadTestObservationIndexFile(path, head string) (testObservationIndex, bool) {
	index, marker, complete := loadTestObservationIndexGeneration(path)
	return index, complete && marker.Head == head
}

func loadTestObservationIndexGeneration(path string) (testObservationIndex, testObservationIndexEvent, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, testObservationIndexEvent{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, testObservationIndexEvent{}, false
	}
	index := make(testObservationIndex)
	var marker testObservationIndexEvent
	foundMarker, complete := false, false
	visitReverseJSONLFileLines(file, info.Size(), func(line []byte) bool {
		var event testObservationIndexEvent
		if json.Unmarshal(line, &event) != nil || event.Record != indexRecord {
			return false
		}
		if !foundMarker {
			if event.Kind != "head" || event.Generation == "" || event.Head == "" {
				return false
			}
			marker, foundMarker = event, true
			return false
		}
		if event.Generation != marker.Generation {
			return false
		}
		switch event.Kind {
		case "observation":
			if event.Key != "" && event.Observation != nil && event.Observation.Schema == testObservationSchema {
				if _, exists := index[event.Key]; !exists {
					index[event.Key] = *event.Observation
				}
			}
		case "start":
			complete = true
			return true
		}
		return false
	})
	if !complete {
		return nil, testObservationIndexEvent{}, false
	}
	return index, marker, true
}

func rebuildTestObservationIndex(repository, head string) (testObservationIndex, error) {
	commits, err := gitOutput(repository, nil, nil, "rev-list", "--reverse", head)
	if err != nil {
		return nil, err
	}
	index := make(testObservationIndex)
	for _, commit := range strings.Fields(commits) {
		contents, err := gitOutput(repository, nil, nil, "show", commit+":observation.json")
		if err != nil {
			return nil, err
		}
		var observation testObservation
		if json.Unmarshal([]byte(contents), &observation) != nil || observation.Schema != testObservationSchema {
			return nil, fmt.Errorf("decoding test observation %s", commit)
		}
		index[testObservationIndexKey(observation.Worktree, observation.Mode, observation.Command)] = observation
	}
	if path, err := testObservationIndexPath(repository); err == nil {
		writeTestObservationIndexGeneration(path, head, index)
	}
	return index, nil
}

func writeTestObservationIndexGeneration(path, head string, index testObservationIndex) {
	generation, err := stateEventID()
	if err != nil {
		return
	}
	events := make([]testObservationIndexEvent, 0, len(index)+2)
	events = append(events, testObservationIndexEvent{Record: indexRecord, Kind: "start", Generation: generation})
	for key, observation := range index {
		value := observation
		events = append(events, testObservationIndexEvent{Record: indexRecord, Kind: "observation", Generation: generation, Key: key, Observation: &value})
	}
	events = append(events, testObservationIndexEvent{Record: indexRecord, Kind: "head", Generation: generation, Head: head})
	_ = appendTestObservationIndexEvents(path, events)
}

func appendTestObservationIndexEvents(path string, events []testObservationIndexEvent) error {
	contents := make([]byte, 0)
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		contents = append(contents, line...)
		contents = append(contents, '\n')
	}
	return appendJSONL(path, contents)
}

func testObservationIndexPath(repository string) (string, error) {
	path, err := gitOutput(repository, nil, nil, "rev-parse", "--git-path", testObservationIndexPathname)
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(repository, path)
	}
	return path, nil
}

func appendJSONL(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if info.Size() > 0 {
		last := []byte{0}
		if _, err := file.ReadAt(last, info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			contents = append([]byte{'\n'}, contents...)
		}
	}
	_, err = file.Write(contents)
	return err
}

func boundedJSONLTail(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const limit = 64 << 10
	start := info.Size() - limit
	if start < 0 {
		start = 0
	}
	contents := make([]byte, info.Size()-start)
	_, err = file.ReadAt(contents, start)
	return contents, err
}

func visitReverseJSONLFileLines(file *os.File, position int64, visit func([]byte) bool) bool {
	const chunkSize = 4096
	var partial []byte
	for position > 0 {
		start := position - chunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, position-start)
		if _, err := file.ReadAt(chunk, start); err != nil {
			return false
		}
		parts := bytes.Split(append(chunk, partial...), []byte{'\n'})
		first := 1
		if start == 0 {
			first = 0
		}
		for index := len(parts) - 1; index >= first; index-- {
			line := bytes.TrimSpace(parts[index])
			if len(line) != 0 && visit(line) {
				return true
			}
		}
		partial = parts[0]
		position = start
	}
	return false
}

func testObservationIndexKey(worktree string, mode testMode, command []string) string {
	if canonical, err := filepath.EvalSymlinks(worktree); err == nil {
		worktree = canonical
	}
	parts := append([]string{worktree, string(mode)}, command...)
	var key strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&key, "%d:%s", len(part), part)
	}
	return key.String()
}
