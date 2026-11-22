package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPreviousTestObservationWarmLookupUsesConstantGitProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scale test in short mode")
	}
	counts := make([]int, 0, 2)
	for _, historySize := range []int{1, 32} {
		t.Run(strconv.Itoa(historySize), func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			command := []string{"sh", "-c", "exit 1"}
			for range historySize {
				if _, err := appendTestObservation(repository, testObservation{
					Schema:     testObservationSchema,
					Worktree:   repository,
					Mode:       testModeTargeted,
					Command:    command,
					ExitStatus: 1,
					ObservedAt: "2026-08-29T00:00:00Z",
				}); err != nil {
					t.Fatal(err)
				}
			}
			head := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
			count := countGitProcesses(t, func() {
				observation, found, err := previousTestObservation(repository, head, repository, testModeTargeted, command)
				if err != nil || !found || observation.ExitStatus != 1 {
					t.Fatalf("previous observation = %#v, %t, %v", observation, found, err)
				}
			})
			if count != 1 {
				t.Fatalf("warm lookup started %d git processes, want 1", count)
			}
			counts = append(counts, count)
		})
	}
	if counts[0] != counts[1] {
		t.Fatalf("warm lookup git processes = %v, want constant cost", counts)
	}
}

func TestPreviousTestObservationRebuildsAfterCacheDeletion(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "exit 1"}
	if _, err := appendTestObservation(repository, testObservation{
		Schema:     testObservationSchema,
		Worktree:   repository,
		Mode:       testModeTargeted,
		Command:    command,
		ExitStatus: 1,
		ObservedAt: "2026-08-29T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	path, err := testObservationIndexPath(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
	observation, found, err := previousTestObservation(repository, head, repository, testModeTargeted, command)
	if err != nil || !found || observation.ExitStatus != 1 {
		t.Fatalf("previous observation = %#v, %t, %v", observation, found, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rebuilt cache missing: %v", err)
	}
}

func TestPreviousTestObservationRebuildsAfterObservationRefChanges(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "test -f green"}
	if _, err := appendTestObservation(repository, testObservation{
		Schema:     testObservationSchema,
		Worktree:   repository,
		Mode:       testModeTargeted,
		Command:    command,
		ExitStatus: 1,
		ObservedAt: "2026-08-29T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	redHead := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
	if _, err := appendTestObservation(repository, testObservation{
		Schema:     testObservationSchema,
		Worktree:   repository,
		Mode:       testModeTargeted,
		Command:    command,
		ExitStatus: 0,
		ObservedAt: "2026-08-29T00:00:01Z",
	}); err != nil {
		t.Fatal(err)
	}
	runTestRunnerGit(t, repository, "update-ref", testObservationRef, redHead)

	observation, found, err := previousTestObservation(repository, redHead, repository, testModeTargeted, command)
	if err != nil || !found || observation.ExitStatus != 1 {
		t.Fatalf("previous observation after ref replacement = %#v, %t, %v", observation, found, err)
	}
}

func TestPreviousTestObservationRepairsPartialCacheWrite(t *testing.T) {
	repository := newTestRunnerRepository(t)
	command := []string{"sh", "-c", "exit 1"}
	if _, err := appendTestObservation(repository, testObservation{
		Schema:     testObservationSchema,
		Worktree:   repository,
		Mode:       testModeTargeted,
		Command:    command,
		ExitStatus: 1,
		ObservedAt: "2026-08-29T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	path, err := testObservationIndexPath(repository)
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
	head := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
	if _, found, err := previousTestObservation(repository, head, repository, testModeTargeted, command); err != nil || !found {
		t.Fatalf("repair lookup = %t, %v", found, err)
	}
	count := countGitProcesses(t, func() {
		if _, found, err := previousTestObservation(repository, head, repository, testModeTargeted, command); err != nil || !found {
			t.Fatalf("warm lookup = %t, %v", found, err)
		}
	})
	if count != 1 {
		t.Fatalf("repaired warm lookup started %d git processes, want 1", count)
	}
}

func TestPreviousTestObservationLoadsLargeEventGeneration(t *testing.T) {
	repository := newTestRunnerRepository(t)
	index := make(testObservationIndex)
	for number := range 1000 {
		command := []string{"test", strings.Repeat("x", 80), strconv.Itoa(number)}
		index[testObservationIndexKey(repository, testModeTargeted, command)] = testObservation{
			Schema:     testObservationSchema,
			Worktree:   repository,
			Mode:       testModeTargeted,
			Command:    command,
			ExitStatus: 1,
			ObservedAt: "2026-08-29T00:00:00Z",
		}
	}
	head := strings.Repeat("a", 40)
	path, err := testObservationIndexPath(repository)
	if err != nil {
		t.Fatal(err)
	}
	writeTestObservationIndexGeneration(path, head, index)
	appendSteeringReset(path)
	loaded, current := loadTestObservationIndexFile(path, head)
	if !current || len(loaded) != len(index) {
		t.Fatalf("large generation loaded %d entries, current %t; want %d, true", len(loaded), current, len(index))
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), `"observations":{`) {
		t.Fatal("index persisted a cumulative observation-map snapshot")
	}
}

func countGitProcesses(t *testing.T, action func()) int {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	countPath := filepath.Join(directory, "count")
	scriptPath := filepath.Join(directory, "git")
	script := "#!/bin/sh\nprintf x >> \"$ATD_TEST_GIT_COUNT\"\nexec \"$ATD_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATD_TEST_GIT_COUNT", countPath)
	t.Setenv("ATD_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	action()
	contents, err := os.ReadFile(countPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(contents)
}
