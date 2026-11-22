package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
)

const harnessConfigKey = "atd.harness"

type harnessInstallation struct {
	harness      harness
	hooks        []byte
	hooksChanged bool
	mode         os.FileMode
	skills       map[string][]byte
	retired      []string
	refusals     []string
}

func enableHarness(repository string, h harness, output io.Writer) error {
	configuration, err := readHookConfiguration(repository, h)
	if err != nil {
		return err
	}
	_, groups, err := preToolUseGroups(h, configuration.values)
	if err != nil {
		return err
	}
	owned, err := ownedHookHandlers(h, groups)
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		if err := recordRepositoryHarness(repository, h); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "atd is already enabled for %s in this worktree; run atd update to refresh it\n", h.label)
		return err
	}
	installation, err := planInstallation(repository, h)
	if err != nil {
		return err
	}
	if err := refuseEditedSkills([]harnessInstallation{installation}); err != nil {
		return err
	}
	if err := applyInstallation(repository, installation); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, h.enabledNotice)
	return err
}

func updateHarnesses(repository string, output io.Writer) error {
	enabled, unknown, err := repositoryHarnesses(repository)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return errors.New(strings.Join(unknownHarnessNotices(unknown), "; "))
	}
	if len(enabled) == 0 {
		return errors.New("atd is not enabled in this repository; run atd enable claude or atd enable codex")
	}
	var installations []harnessInstallation
	for _, h := range enabled {
		installation, err := planInstallation(repository, h)
		if err != nil {
			return err
		}
		installations = append(installations, installation)
	}
	if err := refuseEditedSkills(installations); err != nil {
		return err
	}
	for _, installation := range installations {
		if err := applyInstallation(repository, installation); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "updated atd for %s in this worktree\n", installation.harness.label); err != nil {
			return err
		}
		if installation.harness.name == "codex" && installation.hooksChanged {
			if _, err := fmt.Fprintln(output, "Codex hooks changed; review and trust them with /hooks"); err != nil {
				return err
			}
		}
	}
	return nil
}

func planInstallation(repository string, h harness) (harnessInstallation, error) {
	atdPath, err := currentATDPath()
	if err != nil {
		return harnessInstallation{}, err
	}
	configuration, err := readHookConfiguration(repository, h)
	if err != nil {
		return harnessInstallation{}, err
	}
	_, currentGroups, err := preToolUseGroups(h, configuration.values)
	if err != nil {
		return harnessInstallation{}, err
	}
	currentHooks, err := ownedHookHandlers(h, currentGroups)
	if err != nil {
		return harnessInstallation{}, err
	}
	expectedHooks, err := ownedHookHandlers(h, h.hookGroups(atdPath))
	if err != nil {
		return harnessInstallation{}, err
	}
	hooks, err := reconciledHooks(h, configuration.values, atdPath)
	if err != nil {
		return harnessInstallation{}, err
	}
	rendered, err := renderSkills()
	if err != nil {
		return harnessInstallation{}, err
	}
	skills, err := inspectSkills(repository, h, rendered)
	if err != nil {
		return harnessInstallation{}, err
	}
	installation := harnessInstallation{harness: h, hooks: hooks, hooksChanged: !sameOwnedHookHandlers(currentHooks, expectedHooks), mode: configuration.mode, skills: map[string][]byte{}}
	tracked, err := trackedHarnessFiles(repository, h)
	if err != nil {
		return harnessInstallation{}, err
	}
	for _, path := range tracked {
		installation.refusals = append(installation.refusals, path+" (tracked by Git)")
	}
	for _, skill := range skills {
		switch skill.state {
		case skillStale, skillMissing:
			installation.skills[skill.name] = rendered[skill.name]
		case skillRetired:
			installation.retired = append(installation.retired, skill.name)
		case skillEdited:
			installation.refusals = append(installation.refusals, skill.path+" (edited or not written by atd)")
		}
	}
	return installation, nil
}

func refuseEditedSkills(installations []harnessInstallation) error {
	var refusals []string
	for _, installation := range installations {
		refusals = append(refusals, installation.refusals...)
	}
	if len(refusals) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to write files atd does not own: %s; move them aside and retry", strings.Join(refusals, ", "))
}

// trackedHarnessFiles finds tracked files at atd's targets: Git exclusion
// cannot hide changes to them, so writing would leak this machine's atd path
// or local skills into the tree test observations record.
func trackedHarnessFiles(repository string, h harness) ([]string, error) {
	output, err := gitOutput(repository, nil, nil, "ls-files", "-z", "--", h.hooksPath, h.skillsRoot+"/"+installedSkillPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("listing tracked %s files: %w", h.label, err)
	}
	var tracked []string
	for _, path := range strings.Split(strings.TrimSuffix(output, "\x00"), "\x00") {
		if path != "" {
			tracked = append(tracked, filepath.FromSlash(path))
		}
	}
	return tracked, nil
}

// applyInstallation excludes atd's files before creating them so they never
// appear in the working tree that test observations record.
func applyInstallation(repository string, installation harnessInstallation) error {
	h := installation.harness
	if err := addExcludePatterns(repository, h); err != nil {
		return err
	}
	if err := removeLeftoverTemporaryFiles(repository, h); err != nil {
		return err
	}
	root := filepath.Join(repository, h.skillsRoot)
	for _, name := range slices.Sorted(maps.Keys(installation.skills)) {
		if err := writeFileAtomically(filepath.Join(root, name, skillFile), installation.skills[name], 0o644); err != nil {
			return err
		}
	}
	for _, name := range installation.retired {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("removing retired skill %s: %w", name, err)
		}
	}
	if err := writeFileAtomically(filepath.Join(repository, h.hooksPath), installation.hooks, installation.mode); err != nil {
		return err
	}
	return recordRepositoryHarness(repository, h)
}

// readExcludeFile reads the exclude file every worktree of the repository shares.
func readExcludeFile(repository string) (string, []byte, error) {
	output, err := gitOutput(repository, nil, nil, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return "", nil, fmt.Errorf("locating Git exclude file: %w", err)
	}
	path := strings.TrimSpace(output)
	contents, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("reading Git exclude file: %w", err)
	}
	return path, contents, nil
}

// addExcludePatterns appends the harness's missing patterns under its marker
// so disable can tell them from identical lines the developer wrote.
func addExcludePatterns(repository string, h harness) error {
	path, contents, err := readExcludeFile(repository)
	if err != nil {
		return err
	}
	missing := missingExcludePatterns(contents, h)
	if len(missing) == 0 {
		return nil
	}
	if len(contents) > 0 && contents[len(contents)-1] != '\n' {
		contents = append(contents, '\n')
	}
	contents = append(contents, strings.Join(append([]string{h.excludeMarker()}, missing...), "\n")+"\n"...)
	return writeFileAtomically(path, contents, 0o644)
}

func missingExcludePatterns(contents []byte, h harness) []string {
	existing := map[string]bool{}
	for _, line := range strings.Split(string(contents), "\n") {
		existing[line] = true
	}
	var missing []string
	for _, pattern := range h.excludePatterns() {
		if !existing[pattern] {
			missing = append(missing, pattern)
		}
	}
	return missing
}

// repositoryHarnesses reads the atd.harness record. Unknown names are returned
// separately so a typo or a newer atd's harness blocks only update.
func repositoryHarnesses(repository string) ([]harness, []string, error) {
	values, err := gitConfigValues(repository, harnessConfigKey)
	if err != nil {
		return nil, nil, err
	}
	var enabled []harness
	var unknown []string
	seen := map[string]bool{}
	for _, name := range strings.Fields(strings.Join(values, "\n")) {
		if seen[name] {
			continue
		}
		seen[name] = true
		if h, ok := harnessNamed(name); ok {
			enabled = append(enabled, h)
		} else {
			unknown = append(unknown, name)
		}
	}
	return enabled, unknown, nil
}

func unknownHarnessNotices(unknown []string) []string {
	var notices []string
	for _, name := range unknown {
		notices = append(notices, fmt.Sprintf("%s names unknown harness %q; remove it with git config --local --fixed-value --unset-all %s %s", harnessConfigKey, name, harnessConfigKey, name))
	}
	return notices
}

func recordRepositoryHarness(repository string, h harness) error {
	enabled, _, err := repositoryHarnesses(repository)
	if err != nil {
		return err
	}
	for _, existing := range enabled {
		if existing.name == h.name {
			return nil
		}
	}
	if _, err := gitOutput(repository, nil, nil, "config", "--local", "--add", harnessConfigKey, h.name); err != nil {
		return fmt.Errorf("recording %s: %w", harnessConfigKey, err)
	}
	return nil
}

func writeStatus(repository string, output io.Writer) error {
	if _, err := fmt.Fprintln(output, "atd", buildVersion()); err != nil {
		return err
	}
	scope, err := repositoryScopeStatus(repository)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, scope); err != nil {
		return err
	}
	enabled, unknown, err := repositoryHarnesses(repository)
	if err != nil {
		return err
	}
	for _, notice := range unknownHarnessNotices(unknown) {
		if _, err := fmt.Fprintln(output, notice); err != nil {
			return err
		}
	}
	for _, h := range harnesses {
		repositoryEnabled := false
		for _, existing := range enabled {
			repositoryEnabled = repositoryEnabled || existing.name == h.name
		}
		if _, err := fmt.Fprintln(output, h.name+": "+harnessStatus(repository, h, repositoryEnabled)); err != nil {
			return err
		}
	}
	return nil
}

func harnessStatus(repository string, h harness, repositoryEnabled bool) string {
	fields := []string{"repository not enabled"}
	if repositoryEnabled {
		fields[0] = "repository enabled"
	}
	worktree, err := inspectWorktree(repository, h)
	switch {
	case err != nil:
		fields = append(fields, err.Error())
	case !worktree.enabled:
		fields = append(fields, "worktree not enabled")
		if repositoryEnabled {
			fields = append(fields, "run atd enable "+h.name)
		}
	default:
		fields = append(append(fields, "worktree enabled"), worktree.details...)
		if !repositoryEnabled {
			fields = append(fields, "run atd enable "+h.name)
		} else if worktree.needsUpdate {
			fields = append(fields, "run atd update")
		}
	}
	return strings.Join(fields, "; ")
}

type worktreeInspection struct {
	enabled     bool
	needsUpdate bool
	details     []string
}

func inspectWorktree(repository string, h harness) (worktreeInspection, error) {
	configuration, err := readHookConfiguration(repository, h)
	if err != nil {
		return worktreeInspection{}, err
	}
	_, groups, err := preToolUseGroups(h, configuration.values)
	if err != nil {
		return worktreeInspection{}, err
	}
	owned, err := ownedHookHandlers(h, groups)
	if err != nil || len(owned) == 0 {
		return worktreeInspection{}, err
	}
	atdPath, err := currentATDPath()
	if err != nil {
		return worktreeInspection{}, err
	}
	expected, err := ownedHookHandlers(h, h.hookGroups(atdPath))
	if err != nil {
		return worktreeInspection{}, err
	}
	inspection := worktreeInspection{enabled: true, details: []string{"hooks current"}}
	if !sameOwnedHookHandlers(owned, expected) {
		inspection.details[0] = "hooks stale"
		inspection.needsUpdate = true
	}
	rendered, err := renderSkills()
	if err != nil {
		return worktreeInspection{}, err
	}
	skills, err := inspectSkills(repository, h, rendered)
	if err != nil {
		return worktreeInspection{}, err
	}
	byState := map[skillState][]string{}
	for _, skill := range skills {
		byState[skill.state] = append(byState[skill.state], skill.name)
	}
	skillsCurrent := true
	for _, state := range []skillState{skillStale, skillMissing, skillRetired, skillEdited} {
		if names := byState[state]; len(names) > 0 {
			skillsCurrent = false
			inspection.details = append(inspection.details, "skills "+string(state)+" "+strings.Join(names, ", "))
		}
	}
	if skillsCurrent {
		inspection.details = append(inspection.details, "skills current")
	}
	inspection.needsUpdate = inspection.needsUpdate || !skillsCurrent
	_, excludes, err := readExcludeFile(repository)
	if err != nil {
		return worktreeInspection{}, err
	}
	if missing := missingExcludePatterns(excludes, h); len(missing) > 0 {
		inspection.details = append(inspection.details, "excludes missing "+strings.Join(missing, ", "))
		inspection.needsUpdate = true
	}
	return inspection, nil
}

func sameOwnedHookHandlers(installed, expected []ownedHookHandler) bool {
	sortHandlers := func(handlers []ownedHookHandler) []ownedHookHandler {
		sorted := append([]ownedHookHandler{}, handlers...)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].matcher != sorted[j].matcher {
				return sorted[i].matcher < sorted[j].matcher
			}
			return sorted[i].handler < sorted[j].handler
		})
		return sorted
	}
	return reflect.DeepEqual(sortHandlers(installed), sortHandlers(expected))
}
