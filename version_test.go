package main

import (
	"bytes"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestVersionReportsBuildVersionOutsideRepository(t *testing.T) {
	withTestRunnerDirectory(t, t.TempDir())
	var output bytes.Buffer
	if err := runCLI([]string{"version"}, &output); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^atd \S+\n$`).Match(output.Bytes()) {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestVersionCheckComparesWithLatestReleaseTag(t *testing.T) {
	releases := newTestRunnerRepository(t)
	for _, tag := range []string{"v0.1.0", "v0.2.0", "v0.10.0", "v1.0.0-rc1", "v3-final"} {
		runTestRunnerGit(t, releases, "tag", tag)
	}
	previous := releaseRepository
	releaseRepository = releases
	t.Cleanup(func() { releaseRepository = previous })
	withTestRunnerDirectory(t, t.TempDir())
	install := "go install github.com/markhilkert/atd@v0.10.0"

	for _, test := range []struct {
		current  string
		upToDate bool
		want     string
	}{
		{current: "v0.10.0", upToDate: true, want: "latest release"},
		{current: "v0.10.0+dirty", upToDate: true, want: "latest release"},
		{current: "v0.10.1-0.20260929195954-0123456789ab", upToDate: true, want: "newer than the latest release v0.10.0"},
		{current: "v0.2.0", want: install},
		{current: "v0.10.0-0.20260929195954-0123456789ab", want: install},
		{current: "(devel)", want: "cannot compare"},
	} {
		t.Run(test.current, func(t *testing.T) {
			useBuildVersionForTest(t, test.current)
			var output bytes.Buffer
			err := runCLI([]string{"version", "--check"}, &output)
			if test.upToDate {
				if err != nil {
					t.Fatalf("version --check for current build: %v", err)
				}
				if !strings.Contains(output.String(), test.want) {
					t.Fatalf("version --check output = %q, want %q", output.String(), test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("version --check error = %v, want %q (output %q)", err, test.want, output.String())
			}
		})
	}
}

func TestVersionCheckReportsUnreachableReleases(t *testing.T) {
	previous := releaseRepository
	releaseRepository = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { releaseRepository = previous })
	useBuildVersionForTest(t, "v0.1.0")
	withTestRunnerDirectory(t, t.TempDir())
	var output bytes.Buffer
	err := runCLI([]string{"version", "--check"}, &output)
	if err == nil || !strings.Contains(err.Error(), "could not check") {
		t.Fatalf("version --check with unreachable releases: err=%v output=%q", err, output.String())
	}
}

func TestVersionCheckTimesOutOnStalledRemote(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections []net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connections = append(connections, connection)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	previousRepository, previousTimeout := releaseRepository, releaseCheckTimeout
	releaseRepository, releaseCheckTimeout = "http://"+listener.Addr().String()+"/atd", 500*time.Millisecond
	t.Cleanup(func() { releaseRepository, releaseCheckTimeout = previousRepository, previousTimeout })
	useBuildVersionForTest(t, "v0.1.0")
	withTestRunnerDirectory(t, t.TempDir())

	result := make(chan error, 1)
	go func() { result <- runCLI([]string{"version", "--check"}, io.Discard) }()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("version --check against a stalled remote: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("version --check did not return after its timeout")
	}
}

func TestVersionCommandSurface(t *testing.T) {
	withTestRunnerDirectory(t, t.TempDir())
	for _, arguments := range [][]string{{"version", "--bogus"}, {"version", "--check", "--check"}, {"version", "check"}} {
		if err := runCLI(arguments, io.Discard); err == nil {
			t.Fatalf("atd %s succeeded", strings.Join(arguments, " "))
		}
	}
}

func useBuildVersionForTest(t *testing.T, version string) {
	t.Helper()
	previous := currentBuildVersion
	currentBuildVersion = func() string { return version }
	t.Cleanup(func() { currentBuildVersion = previous })
}
