package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const hooksFileMode = 0o600

var currentExecutable = os.Executable

type harness struct {
	name           string
	label          string
	hookSubcommand string
	hooksPath      string
	skillsRoot     string
	editMatcher    string
	commandMatcher string
	asyncField     bool
	enabledNotice  string
	// hostExcludes are harness-created paths outside atd's files that must stay
	// out of test snapshots.
	hostExcludes []string
}

var harnesses = []harness{
	{
		name:           "claude",
		label:          "Claude Code",
		hookSubcommand: "_claude-hook",
		hooksPath:      ".claude/settings.local.json",
		skillsRoot:     ".claude/skills",
		commandMatcher: "Bash",
		editMatcher:    "Edit|Write|NotebookEdit",
		enabledNotice:  "enabled atd for Claude Code in this worktree; start a new Claude Code session to load its hooks and skills",
		// Subagent worktrees are nested repositories that snapshots would record as gitlinks.
		hostExcludes: []string{"/.claude/worktrees/"},
	},
	{
		name:           "codex",
		label:          "Codex",
		hookSubcommand: "_codex-hook",
		hooksPath:      ".codex/hooks.json",
		skillsRoot:     ".agents/skills",
		commandMatcher: "^Bash$",
		editMatcher:    "^(apply_patch|Edit|Write)$",
		asyncField:     true,
		enabledNotice:  "enabled atd for Codex in this worktree; review and trust its hooks with /hooks",
	},
}

func harnessNamed(name string) (harness, bool) {
	for _, harness := range harnesses {
		if harness.name == name {
			return harness, true
		}
	}
	return harness{}, false
}

func (h harness) excludePatterns() []string {
	return append([]string{"/" + h.hooksPath, "/" + path.Dir(h.hooksPath) + "/" + temporaryFilePrefix + "*", "/" + h.skillsRoot + "/" + installedSkillPrefix + "*/"}, h.hostExcludes...)
}

func (h harness) excludeMarker() string {
	return "# atd " + h.name
}

func (h harness) hookGroups(atdPath string) []any {
	return []any{
		map[string]any{"matcher": h.commandMatcher, "hooks": []any{h.hookHandler(atdPath, steeringTriggerCommand)}},
		map[string]any{"matcher": h.editMatcher, "hooks": []any{h.hookHandler(atdPath, steeringTriggerEdit)}},
	}
}

func (h harness) hookHandler(atdPath string, trigger steeringTrigger) map[string]any {
	command := shellQuote(atdPath) + " " + h.hookSubcommand + " " + string(trigger) + " " + h.hookMarker(trigger)
	handler := map[string]any{"type": "command", "command": command, "timeout": json.Number("10")}
	if h.asyncField {
		handler["async"] = false
	}
	return handler
}

func (h harness) hookMarker(trigger steeringTrigger) string {
	return "# atd:" + h.name + "-pre-tool-use:" + string(trigger) + ":v1"
}

func (h harness) ownedHook(value any) bool {
	handler, ok := value.(map[string]any)
	if !ok || handler["type"] != "command" {
		return false
	}
	command, ok := handler["command"].(string)
	return ok && (strings.HasSuffix(command, h.hookMarker(steeringTriggerCommand)) || strings.HasSuffix(command, h.hookMarker(steeringTriggerEdit)))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func currentATDPath() (string, error) {
	path, err := currentExecutable()
	if err != nil {
		return "", fmt.Errorf("resolving current atd executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving current atd executable: %w", err)
	}
	return path, nil
}

type hookConfiguration struct {
	values map[string]any
	mode   os.FileMode
	exists bool
}

func readHookConfiguration(repository string, h harness) (hookConfiguration, error) {
	if err := requireRealDirectories(repository, filepath.Dir(h.hooksPath)); err != nil {
		return hookConfiguration{}, err
	}
	path := filepath.Join(repository, h.hooksPath)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return hookConfiguration{values: map[string]any{}, mode: hooksFileMode}, nil
	}
	if err != nil {
		return hookConfiguration{}, fmt.Errorf("inspecting %s hooks: %w", h.label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return hookConfiguration{}, fmt.Errorf("%s must be a non-symlink regular file", h.hooksPath)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return hookConfiguration{}, fmt.Errorf("reading existing %s hooks: %w", h.label, err)
	}
	values, err := decodeHookConfiguration(h, contents)
	if err != nil {
		return hookConfiguration{}, err
	}
	return hookConfiguration{values: values, mode: info.Mode().Perm(), exists: true}, nil
}

func decodeHookConfiguration(h harness, contents []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("reading existing %s hooks: %w", h.label, err)
	}
	if values == nil {
		return nil, fmt.Errorf("existing %s hooks must be an object", h.label)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("reading existing %s hooks: multiple JSON values", h.label)
		}
		return nil, fmt.Errorf("reading existing %s hooks: %w", h.label, err)
	}
	return values, nil
}

func preToolUseGroups(h harness, values map[string]any) (map[string]any, []any, error) {
	hooks := map[string]any{}
	if value, exists := values["hooks"]; exists {
		var ok bool
		if hooks, ok = value.(map[string]any); !ok {
			return nil, nil, fmt.Errorf("existing %s hooks must be an object", h.label)
		}
	}
	value, exists := hooks["PreToolUse"]
	if !exists {
		return hooks, nil, nil
	}
	groups, ok := value.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("existing %s PreToolUse hooks must be an array", h.label)
	}
	for _, value := range groups {
		group, ok := value.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("existing %s PreToolUse group must be an object", h.label)
		}
		if _, ok := group["hooks"].([]any); !ok {
			return nil, nil, fmt.Errorf("existing %s PreToolUse group must contain a hooks array", h.label)
		}
	}
	return hooks, groups, nil
}

// reconciledHooks replaces atd-owned handlers with the current ones and keeps
// every other group, handler and field.
func reconciledHooks(h harness, values map[string]any, atdPath string) ([]byte, error) {
	hooks, groups, _, err := withoutOwnedHooks(h, values)
	if err != nil {
		return nil, err
	}
	hooks["PreToolUse"] = append(groups, h.hookGroups(atdPath)...)
	values["hooks"] = hooks
	return encodeHookConfiguration(values)
}

// withoutOwnedHooks returns the hooks object and the PreToolUse groups left
// after removing atd-owned handlers, dropping groups that held nothing else.
func withoutOwnedHooks(h harness, values map[string]any) (map[string]any, []any, int, error) {
	hooks, groups, err := preToolUseGroups(h, values)
	if err != nil {
		return nil, nil, 0, err
	}
	var remaining []any
	removed := 0
	for _, value := range groups {
		group := value.(map[string]any)
		handlers := group["hooks"].([]any)
		kept := make([]any, 0, len(handlers))
		for _, handler := range handlers {
			if !h.ownedHook(handler) {
				kept = append(kept, handler)
			}
		}
		removed += len(handlers) - len(kept)
		if len(kept) == len(handlers) {
			remaining = append(remaining, value)
			continue
		}
		if len(kept) == 0 && len(group) == 2 {
			continue
		}
		copy := make(map[string]any, len(group))
		for key, item := range group {
			copy[key] = item
		}
		copy["hooks"] = kept
		remaining = append(remaining, copy)
	}
	return hooks, remaining, removed, nil
}

func encodeHookConfiguration(values map[string]any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(values); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

type ownedHookHandler struct {
	matcher string
	handler string
}

func ownedHookHandlers(h harness, groups []any) ([]ownedHookHandler, error) {
	var owned []ownedHookHandler
	for _, value := range groups {
		group := value.(map[string]any)
		matcher, _ := group["matcher"].(string)
		for _, handler := range group["hooks"].([]any) {
			if !h.ownedHook(handler) {
				continue
			}
			encoded, err := json.Marshal(handler)
			if err != nil {
				return nil, err
			}
			owned = append(owned, ownedHookHandler{matcher: matcher, handler: string(encoded)})
		}
	}
	return owned, nil
}

// removeLeftoverTemporaryFiles cleans the directories atd writes for a harness.
func removeLeftoverTemporaryFiles(repository string, h harness) error {
	directories := []string{filepath.Join(repository, filepath.Dir(h.hooksPath))}
	root := filepath.Join(repository, h.skillsRoot)
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", h.skillsRoot, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), installedSkillPrefix) && entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	for _, directory := range directories {
		if err := removeTemporaryFiles(directory); err != nil {
			return err
		}
	}
	return nil
}

func writeHookContext(output io.Writer, context string) error {
	return json.NewEncoder(output).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PreToolUse",
			"additionalContext": context,
		},
	})
}
