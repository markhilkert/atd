package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWithoutScopePreservesTheExistingScope(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)

	if err := runCLI([]string{"init"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := scopeConfigValuesForTest(t, repository); len(got) != 0 {
		t.Fatalf("ordinary init configured scope: %q", got)
	}
	if got := scopeStatusLineForTest(t); got != "scope: unconfigured" {
		t.Fatalf("ordinary init status = %q", got)
	}

	if err := runCLI([]string{"init", "--scope", "owner-controlled"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"init"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := scopeConfigValuesForTest(t, repository); len(got) != 1 || got[0] != "owner-controlled" {
		t.Fatalf("ordinary init changed configured scope: %q", got)
	}
	if got := scopeStatusLineForTest(t); got != "scope: owner-controlled" {
		t.Fatalf("configured status = %q", got)
	}
}

func TestInitSetsAndExplicitlyChangesRepositoryScopeWithoutReplacingScripts(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	customScripts := map[string]struct {
		contents string
		mode     os.FileMode
	}{
		"test_fast": {contents: "#!/bin/sh\nexit 21\n", mode: 0o751},
		"test":      {contents: "#!/bin/sh\nexit 22\n", mode: 0o754},
	}

	for index, scope := range []string{"owner-controlled", "owner-controlled", "ticket-bounded"} {
		if err := runCLI([]string{"init", "--scope", scope}, io.Discard); err != nil {
			t.Fatalf("init scope %s: %v", scope, err)
		}
		if index == 0 {
			for name, script := range customScripts {
				path := filepath.Join(repository, "script", name)
				if err := os.WriteFile(path, []byte(script.contents), script.mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, script.mode); err != nil {
					t.Fatal(err)
				}
			}
		}
		if got := scopeConfigValuesForTest(t, repository); len(got) != 1 || got[0] != scope {
			t.Fatalf("configured scope after %s = %q", scope, got)
		}
		if got := scopeStatusLineForTest(t); got != "scope: "+scope {
			t.Fatalf("status after %s = %q", scope, got)
		}
	}

	for name, script := range customScripts {
		path := filepath.Join(repository, "script", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != script.contents {
			t.Fatalf("%s content changed: %q", path, contents)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != script.mode {
			t.Fatalf("%s mode changed: %o", path, info.Mode().Perm())
		}
	}
}

func TestInitRejectsInvalidScopeBeforeWritingScriptsOrConfiguration(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
	}{
		{name: "missing value", arguments: []string{"init", "--scope"}},
		{name: "unknown value", arguments: []string{"init", "--scope", "unrestricted"}},
		{name: "duplicate option", arguments: []string{"init", "--scope", "owner-controlled", "--scope", "ticket-bounded"}},
		{name: "extra argument", arguments: []string{"init", "--scope", "owner-controlled", "extra"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI(test.arguments, io.Discard); err == nil {
				t.Fatal("invalid init scope succeeded")
			}
			if _, err := os.Stat(filepath.Join(repository, "script")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid init created script directory: %v", err)
			}
			if got := scopeConfigValuesForTest(t, repository); len(got) != 0 {
				t.Fatalf("invalid init wrote scope %q", got)
			}
		})
	}
}

func TestInitDoesNotChangeScopeWhenScriptValidationFails(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	runTestRunnerGit(t, repository, "config", "--local", "atd.scope", "owner-controlled")
	if err := os.Mkdir(filepath.Join(repository, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "script", "test_fast"), []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"init", "--scope", "ticket-bounded"}, io.Discard); err == nil {
		t.Fatal("init accepted a non-executable test wrapper")
	}
	if got := scopeConfigValuesForTest(t, repository); len(got) != 1 || got[0] != "owner-controlled" {
		t.Fatalf("failed init changed scope: %q", got)
	}
}

func TestScopeIgnoresGlobalGitConfiguration(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	global := filepath.Join(t.TempDir(), "global.gitconfig")
	if err := os.WriteFile(global, []byte("[atd]\n\tscope = owner-controlled\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if got := scopeStatusLineForTest(t); got != "scope: unconfigured" {
		t.Fatalf("global scope granted repository authority: %q", got)
	}
	if err := runCLI([]string{"init", "--scope", "ticket-bounded"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := scopeStatusLineForTest(t); got != "scope: ticket-bounded" {
		t.Fatalf("local scope lost to global value: %q", got)
	}
}

func TestStatusDoesNotGrantAuthorityForUnknownOrDuplicateScope(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
	}{
		{name: "unknown", values: []string{"unrestricted"}},
		{name: "duplicate valid", values: []string{"owner-controlled", "owner-controlled"}},
		{name: "conflicting", values: []string{"owner-controlled", "ticket-bounded"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			withTestRunnerDirectory(t, repository)
			for _, value := range test.values {
				runTestRunnerGit(t, repository, "config", "--local", "--add", "atd.scope", value)
			}
			if got := scopeStatusLineForTest(t); !strings.HasPrefix(got, "scope: invalid") {
				t.Fatalf("%s scope granted authority or hid invalid configuration: %q", test.name, got)
			}
			var output bytes.Buffer
			if err := runCLI([]string{"status"}, &output); err != nil {
				t.Fatal(err)
			}
			for _, harness := range []string{"claude:", "codex:"} {
				if !strings.Contains(output.String(), harness) {
					t.Fatalf("invalid scope suppressed %s status: %q", harness, output.String())
				}
			}
		})
	}
}

func TestRepositoryScopeIsSharedByLinkedWorktrees(t *testing.T) {
	repository := newTestRunnerRepository(t)
	withTestRunnerDirectory(t, repository)
	if err := runCLI([]string{"init", "--scope", "ticket-bounded"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	worktree := newLinkedWorktreeForTest(t, repository)
	withTestRunnerDirectory(t, worktree)
	if got := scopeStatusLineForTest(t); got != "scope: ticket-bounded" {
		t.Fatalf("linked worktree scope = %q", got)
	}
	if got := scopeConfigValuesForTest(t, worktree); len(got) != 1 || got[0] != "ticket-bounded" {
		t.Fatalf("linked worktree local scope = %q", got)
	}
}

func TestHarnessLifecyclePreservesRepositoryScope(t *testing.T) {
	for _, harness := range harnessesForTest {
		t.Run(harness.name, func(t *testing.T) {
			repository := newTestRunnerRepository(t)
			installFakeATDForTest(t)
			withTestRunnerDirectory(t, repository)
			if err := runCLI([]string{"init", "--scope", "owner-controlled"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			for _, arguments := range [][]string{{"enable", harness.name}, {"update"}, {"disable", harness.name}} {
				if err := runCLI(arguments, io.Discard); err != nil {
					t.Fatalf("%v: %v", arguments, err)
				}
				if got := scopeConfigValuesForTest(t, repository); len(got) != 1 || got[0] != "owner-controlled" {
					t.Fatalf("%v changed scope: %q", arguments, got)
				}
				if got := scopeStatusLineForTest(t); got != "scope: owner-controlled" {
					t.Fatalf("%v status = %q", arguments, got)
				}
			}
		})
	}
}

func scopeConfigValuesForTest(t *testing.T, repository string) []string {
	t.Helper()
	command := exec.Command("git", "-C", repository, "config", "--local", "--get-all", "atd.scope")
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("reading local scope: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
}

func scopeStatusLineForTest(t *testing.T) string {
	t.Helper()
	var output bytes.Buffer
	if err := runCLI([]string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	var found string
	for _, line := range strings.Split(output.String(), "\n") {
		if !strings.HasPrefix(line, "scope:") {
			continue
		}
		if found != "" {
			t.Fatalf("status has multiple scope lines: %q", output.String())
		}
		found = line
	}
	if found == "" {
		t.Fatalf("status has no scope line: %q", output.String())
	}
	return found
}
