package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func parseDisableCommand(arguments []string) (harness, bool, error) {
	usage := errors.New("usage: atd disable [--force] claude | codex")
	var name string
	force := false
	for _, argument := range arguments {
		switch {
		case argument == "--force" && !force:
			force = true
		case !strings.HasPrefix(argument, "-") && name == "":
			name = argument
		default:
			return harness{}, false, usage
		}
	}
	if name == "" {
		return harness{}, false, usage
	}
	h, ok := harnessNamed(name)
	if !ok {
		return harness{}, false, fmt.Errorf("unknown harness %q; %s", name, usage)
	}
	return h, force, nil
}

// disableHarness removes atd's hooks and skills from this worktree. The
// exclude entries and atd.harness record are shared by every worktree, so they
// go only when no other worktree still has the harness installed; a leftover
// record would make atd update reinstall it.
func disableHarness(repository string, h harness, force bool, output io.Writer) error {
	configuration, err := readHookConfiguration(repository, h)
	if err != nil {
		return fmt.Errorf("%s: %w", h.hooksPath, err)
	}
	hooks, groups, removedHooks, err := withoutOwnedHooks(h, configuration.values)
	if err != nil {
		return fmt.Errorf("%s: %w", h.hooksPath, err)
	}
	rendered, err := renderSkills()
	if err != nil {
		return err
	}
	skills, err := inspectSkills(repository, h, rendered)
	if err != nil {
		return err
	}
	var installed, edited []string
	for _, skill := range skills {
		if skill.state == skillMissing {
			continue
		}
		installed = append(installed, skill.name)
		if skill.state == skillEdited {
			edited = append(edited, skill.path)
		}
	}
	if len(edited) > 0 && !force {
		return fmt.Errorf("refusing to remove files atd does not own: %s; rerun with --force to remove them", strings.Join(edited, ", "))
	}
	tracked, err := trackedHarnessFiles(repository, h)
	if err != nil {
		return err
	}
	var refused []string
	for _, path := range tracked {
		if path != filepath.FromSlash(h.hooksPath) || removedHooks > 0 {
			refused = append(refused, path)
		}
	}
	if len(refused) > 0 {
		return fmt.Errorf("refusing to remove atd from files Git tracks: %s", strings.Join(refused, ", "))
	}

	if err := removeLeftoverTemporaryFiles(repository, h); err != nil {
		return err
	}
	if removedHooks > 0 {
		if err := writeHooksWithout(repository, h, configuration, hooks, groups); err != nil {
			return err
		}
	}
	// A missing skill may still have a directory from an interrupted write.
	for _, skill := range skills {
		if err := os.RemoveAll(filepath.Join(repository, h.skillsRoot, skill.name)); err != nil {
			return fmt.Errorf("removing skill %s: %w", skill.name, err)
		}
	}
	notice := "disabled atd for " + h.label + " in this worktree"
	if removedHooks == 0 && len(installed) == 0 {
		notice = "atd is not enabled for " + h.label + " in this worktree"
	}
	if _, err := fmt.Fprintln(output, notice); err != nil {
		return err
	}

	users, err := worktreesUsing(repository, h)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		_, err := fmt.Fprintf(output, "kept the shared Git exclude entries and atd.harness record; %s remains in %s\n", h.label, strings.Join(users, ", "))
		return err
	}
	if err := removeExcludePatterns(repository, h); err != nil {
		return err
	}
	return unrecordRepositoryHarness(repository, h)
}

func writeHooksWithout(repository string, h harness, configuration hookConfiguration, hooks map[string]any, groups []any) error {
	values := configuration.values
	if len(groups) > 0 {
		hooks["PreToolUse"] = groups
	} else {
		delete(hooks, "PreToolUse")
	}
	if len(hooks) > 0 {
		values["hooks"] = hooks
	} else {
		delete(values, "hooks")
	}
	path := filepath.Join(repository, h.hooksPath)
	if len(values) == 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing %s: %w", h.hooksPath, err)
		}
		return nil
	}
	contents, err := encodeHookConfiguration(values)
	if err != nil {
		return err
	}
	return writeFileAtomically(path, contents, configuration.mode)
}

// worktreesUsing lists worktrees with atd hooks or skills for the harness. It
// runs after this worktree's removal, and deleted worktree directories and
// bare repositories read as empty, so none of them counts; one that cannot be inspected counts, so its
// files stay excluded.
func worktreesUsing(repository string, h harness) ([]string, error) {
	output, err := gitOutput(repository, nil, nil, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}
	var users []string
	for _, record := range strings.Split(output, "\x00\x00") {
		path, found := strings.CutPrefix(strings.Split(record, "\x00")[0], "worktree ")
		if !found {
			continue
		}
		using, err := worktreeUsesHarness(path, h)
		switch {
		case err != nil:
			users = append(users, fmt.Sprintf("%s (not inspected: %v)", path, err))
		case using:
			users = append(users, path)
		}
	}
	return users, nil
}

func worktreeUsesHarness(worktree string, h harness) (bool, error) {
	configuration, err := readHookConfiguration(worktree, h)
	if err != nil {
		return false, err
	}
	_, groups, err := preToolUseGroups(h, configuration.values)
	if err != nil {
		return false, err
	}
	owned, err := ownedHookHandlers(h, groups)
	if err != nil || len(owned) > 0 {
		return len(owned) > 0, err
	}
	entries, err := os.ReadDir(filepath.Join(worktree, h.skillsRoot))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), installedSkillPrefix) {
			return true, nil
		}
	}
	return false, nil
}

// removeExcludePatterns removes the harness's markers and the patterns
// directly beneath them, leaving lines the developer wrote.
func removeExcludePatterns(repository string, h harness) error {
	path, contents, err := readExcludeFile(repository)
	if err != nil || len(contents) == 0 {
		return err
	}
	lines := strings.SplitAfter(string(contents), "\n")
	kept := lines[:0]
	owned := false
	for _, line := range lines {
		text := strings.TrimSuffix(line, "\n")
		switch {
		case text == h.excludeMarker():
			owned = true
		case owned && slices.Contains(h.excludePatterns(), text):
		default:
			owned = false
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		return nil
	}
	return writeFileAtomically(path, []byte(strings.Join(kept, "")), 0o644)
}

func unrecordRepositoryHarness(repository string, h harness) error {
	_, err := gitOutput(repository, nil, nil, "config", "--local", "--fixed-value", "--unset-all", harnessConfigKey, h.name)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 5 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("removing %s from %s: %w", h.name, harnessConfigKey, err)
	}
	return nil
}
