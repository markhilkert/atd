package main

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const maxBoundRedSummary = 5
const maxCommandSummaryCharacters = 120

func parseVerifyCommand(arguments []string) (string, string, error) {
	if len(arguments) != 4 {
		return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
	}
	var base, candidate string
	for index := 0; index < len(arguments); index += 2 {
		if arguments[index+1] == "" {
			return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
		}
		switch arguments[index] {
		case "--base":
			if base != "" {
				return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
			}
			base = arguments[index+1]
		case "--candidate":
			if candidate != "" {
				return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
			}
			candidate = arguments[index+1]
		default:
			return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
		}
	}
	if base == "" || candidate == "" {
		return "", "", errors.New("usage: atd verify --base <commit> --candidate <commit>")
	}
	return base, candidate, nil
}

func verifyCandidate(repository, baseArgument, candidateArgument string, output io.Writer) error {
	canonicalRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return fmt.Errorf("canonicalizing repository root: %w", err)
	}
	repository = canonicalRepository
	base, err := resolveCommit(repository, baseArgument)
	if err != nil {
		return fmt.Errorf("resolving base commit: %w", err)
	}
	candidate, err := resolveCommit(repository, candidateArgument)
	if err != nil {
		return fmt.Errorf("resolving candidate commit: %w", err)
	}
	ancestor, err := commitIsAncestor(repository, base, candidate)
	if err != nil {
		return err
	}
	if !ancestor {
		return errors.New("base commit is not an ancestor of candidate")
	}
	rangeCommits, err := commitsInRange(repository, base, candidate)
	if err != nil {
		return err
	}
	observations, err := localTestObservations(repository)
	if err != nil {
		return err
	}
	boundReds, err := boundRedCommits(repository, rangeCommits, observations)
	if err != nil {
		return err
	}
	relevantRedHeads, err := relevantRedObservationHeads(repository, base, rangeCommits)
	if err != nil {
		return err
	}
	candidateTree, err := commitTree(repository, candidate)
	if err != nil {
		return err
	}
	fast := hasCandidateGreen(observations, repository, candidateTree, testModeFast, []string{"./script/" + testFastScript})
	if !fast {
		return errors.New("candidate lacks a stable local green fast observation on its exact tree")
	}
	full := hasCandidateGreen(observations, repository, candidateTree, testModeFull, []string{"./script/" + testFullScript})
	if !full {
		return errors.New("candidate lacks a stable local green full observation on its exact tree")
	}
	printVerificationSummary(output, repository, relevantRedHeads, observations, boundReds)
	return nil
}

func resolveCommit(repository, argument string) (string, error) {
	commit, err := gitOutput(repository, nil, nil, "rev-parse", "--verify", "--quiet", argument+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q is not a commit", argument)
	}
	return strings.TrimSpace(commit), nil
}

func commitIsAncestor(repository, base, candidate string) (bool, error) {
	_, err := gitOutput(repository, nil, nil, "merge-base", "--is-ancestor", base, candidate)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("checking commit ancestry: %w", err)
}

func commitsInRange(repository, base, candidate string) ([]string, error) {
	output, err := gitOutput(repository, nil, nil, "rev-list", "--reverse", base+".."+candidate)
	if err != nil {
		return nil, fmt.Errorf("reading candidate history: %w", err)
	}
	return strings.Fields(output), nil
}

func commitTree(repository, commit string) (string, error) {
	tree, err := gitOutput(repository, nil, nil, "rev-parse", "--verify", "--quiet", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("reading tree for commit %s: %w", commit, err)
	}
	return strings.TrimSpace(tree), nil
}

func commitParent(repository, commit string) (string, bool, error) {
	parents, err := gitOutput(repository, nil, nil, "show", "-s", "--format=%P", commit)
	if err != nil {
		return "", false, fmt.Errorf("reading parents for commit %s: %w", commit, err)
	}
	fields := strings.Fields(parents)
	if len(fields) != 1 {
		return "", false, nil
	}
	return fields[0], true, nil
}

type boundRed struct {
	Commit      string
	Observation testObservation
}

func boundRedCommits(repository string, commits []string, observations []testObservation) ([]boundRed, error) {
	seen := make(map[string]bool)
	var result []boundRed
	for _, commit := range commits {
		parent, hasSoleParent, err := commitParent(repository, commit)
		if err != nil {
			return nil, err
		}
		if !hasSoleParent {
			continue
		}
		tree, err := commitTree(repository, commit)
		if err != nil {
			return nil, err
		}
		for _, observation := range observations {
			if !credibleRed(observation) || !sameWorktree(observation.Worktree, repository) {
				continue
			}
			if observation.BeforeHead == parent && observation.AfterHead == parent && observation.BeforeTree == tree && observation.AfterTree == tree {
				key := commit + "\x00" + redObservationKey(observation)
				if !seen[key] {
					seen[key] = true
					result = append(result, boundRed{Commit: commit, Observation: observation})
				}
			}
		}
	}
	return result, nil
}

func redObservationKey(observation testObservation) string {
	return strings.Join([]string{
		observation.BeforeHead,
		observation.BeforeTree,
		string(observation.Mode),
		strings.Join(observation.Command, "\x00"),
		fmt.Sprint(observation.ExitStatus),
	}, "\x00")
}

func sameWorktree(observed, repository string) bool {
	canonical, err := filepath.EvalSymlinks(observed)
	return err == nil && canonical == repository
}

func hasCandidateGreen(observations []testObservation, repository, tree string, mode testMode, command []string) bool {
	for index := len(observations) - 1; index >= 0; index-- {
		observation := observations[index]
		if observation.BeforeTree == tree && observation.Mode == mode && slices.Equal(observation.Command, command) && sameWorktree(observation.Worktree, repository) {
			return observation.ExitStatus == 0 && !observation.Signaled && !observation.LaunchFailed && stableObservation(observation)
		}
	}
	return false
}

func relevantRedObservationHeads(repository, base string, commits []string) (map[string]bool, error) {
	heads := map[string]bool{base: true}
	for _, commit := range commits {
		heads[commit] = true
		parent, hasSoleParent, err := commitParent(repository, commit)
		if err != nil {
			return nil, err
		}
		if hasSoleParent {
			heads[parent] = true
		}
	}
	return heads, nil
}

func printVerificationSummary(output io.Writer, repository string, relevantHeads map[string]bool, observations []testObservation, boundReds []boundRed) {
	if relevantHeads == nil {
		relevantHeads = map[string]bool{}
	}
	credible := make(map[string]bool)
	var sourceChanges, historyChanges, signals, launchFailures int
	for _, observation := range observations {
		if !relevantHeads[observation.BeforeHead] || !sameWorktree(observation.Worktree, repository) {
			continue
		}
		if credibleRed(observation) {
			credible[redObservationKey(observation)] = true
		}
		if observation.SourceChanged {
			sourceChanges++
		}
		if observation.HistoryChanged {
			historyChanges++
		}
		if observation.Signaled {
			signals++
		}
		if observation.LaunchFailed {
			launchFailures++
		}
	}
	bound := make(map[string]bool)
	for _, red := range boundReds {
		bound[redObservationKey(red.Observation)] = true
	}
	fmt.Fprintln(output, "verification gates passed")
	fmt.Fprintf(output, "credible red observations: %d\nbound red candidates: %d\n", len(credible), len(boundReds))
	for _, red := range boundReds[:min(len(boundReds), maxBoundRedSummary)] {
		fmt.Fprintf(output, "  bound red: %s status %d command %s\n", red.Commit, red.Observation.ExitStatus, summarizeCommand(red.Observation.Command))
	}
	if hidden := len(boundReds) - maxBoundRedSummary; hidden > 0 {
		fmt.Fprintf(output, "  ... %d more bound red candidates; inspect local observations on demand\n", hidden)
	}
	unbound := 0
	for key := range credible {
		if !bound[key] {
			unbound++
		}
	}
	fmt.Fprintf(output, "unbound credible reds: %d\ncandidate gates: fast green; full green\n", unbound)
	fmt.Fprintf(output, "run anomalies (advisory): source changes %d; history changes %d; signals %d; launch failures %d\n", sourceChanges, historyChanges, signals, launchFailures)
	fmt.Fprintln(output, "outer-red evidence is advisory review input")
}

func summarizeCommand(command []string) string {
	quoted := []rune(fmt.Sprintf("%q", command))
	if len(quoted) <= maxCommandSummaryCharacters {
		return string(quoted)
	}
	return string(quoted[:maxCommandSummaryCharacters-3]) + "..."
}
