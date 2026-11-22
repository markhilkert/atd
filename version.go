package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// releaseRepository is where version --check finds release tags; Git's
// insteadOf configuration can rewrite it for private access.
var releaseRepository = "https://github.com/markhilkert/atd"

var currentBuildVersion = buildVersion

var releaseCheckTimeout = 20 * time.Second

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}

func writeVersion(output io.Writer) error {
	_, err := fmt.Fprintln(output, "atd", currentBuildVersion())
	return err
}

// checkVersion compares the build's semantic version core with the latest
// release. A development build's pseudo-version carries the next patch after
// the release it was built on, so a larger core means it is newer and an equal
// core means it predates that release.
func checkVersion(output io.Writer) error {
	current := currentBuildVersion()
	latest, err := latestRelease()
	if err != nil {
		return err
	}
	upgrade := fmt.Sprintf("run go install github.com/markhilkert/atd@%s, then atd update in enabled worktrees", latest)
	core, development, ok := parseVersion(current)
	if !ok {
		return fmt.Errorf("cannot compare build version %s with the latest release %s; %s", current, latest, upgrade)
	}
	latestCore, _, _ := parseVersion(latest)
	comparison := compareVersionCores(core, latestCore)
	switch {
	case comparison > 0 && development:
		_, err = fmt.Fprintf(output, "atd %s is a development build newer than the latest release %s\n", current, latest)
	case comparison > 0:
		_, err = fmt.Fprintf(output, "atd %s is newer than the latest release %s\n", current, latest)
	case comparison == 0 && !development:
		_, err = fmt.Fprintf(output, "atd %s is the latest release\n", current)
	default:
		err = fmt.Errorf("atd %s is older than the latest release %s; %s", current, latest, upgrade)
	}
	return err
}

func latestRelease() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), releaseCheckTimeout)
	defer cancel()
	// The low-speed limit makes Git's HTTP helper give up on its own; killing
	// git on timeout would otherwise orphan it holding the output pipes, which
	// WaitDelay stops us waiting for.
	stall := strconv.Itoa(max(1, int(releaseCheckTimeout.Seconds())))
	command := exec.CommandContext(ctx, "git", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime="+stall, "ls-remote", "--tags", "--refs", releaseRepository)
	command.Env = gitEnvironment(map[string]string{"GIT_TERMINAL_PROMPT": "0"})
	command.WaitDelay = time.Second
	output, err := command.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("could not check %s for releases: timed out after %s", releaseRepository, releaseCheckTimeout)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("could not check %s for releases (it needs the same GitHub access as go install; see the README's Quickstart): %w", releaseRepository, err)
	}
	latest := ""
	var latestCore [3]int
	for _, line := range strings.Split(string(output), "\n") {
		_, ref, _ := strings.Cut(line, "\t")
		tag, found := strings.CutPrefix(ref, "refs/tags/")
		if !found {
			continue
		}
		core, prerelease, ok := parseVersion(tag)
		if ok && !prerelease && (latest == "" || compareVersionCores(core, latestCore) > 0) {
			latest, latestCore = tag, core
		}
	}
	if latest == "" {
		return "", fmt.Errorf("could not check for releases: %s has no vX.Y.Z tags", releaseRepository)
	}
	return latest, nil
}

// parseVersion reads vMAJOR.MINOR.PATCH with optional prerelease and build
// metadata; metadata such as a dirty-tree marker does not affect ordering.
func parseVersion(version string) ([3]int, bool, bool) {
	version, _, _ = strings.Cut(version, "+")
	rest, found := strings.CutPrefix(version, "v")
	if !found {
		return [3]int{}, false, false
	}
	rest, prerelease, development := strings.Cut(rest, "-")
	if development && prerelease == "" {
		return [3]int{}, false, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return [3]int{}, false, false
	}
	var core [3]int
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return [3]int{}, false, false
		}
		core[i] = number
	}
	return core, development, true
}

func compareVersionCores(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}
