package main

import (
	"strings"
	"testing"
)

func validationObservation() testObservation {
	id := strings.Repeat("a", 40)
	return testObservation{
		Schema: testObservationSchema, Worktree: "relative-worktree",
		BeforeTree: id, AfterTree: id, BeforeHead: id, AfterHead: id,
		Mode: testModeFast, Command: []string{"test"},
		ObservedAt: "2026-10-17T00:00:00Z",
	}
}

func TestValidateTestObservationIdentifiesInvalidField(t *testing.T) {
	for _, test := range []struct {
		name, field string
		change      func(*testObservation)
	}{
		{"empty worktree", "worktree", func(o *testObservation) { o.Worktree = "" }},
		{"before tree short", "before_tree", func(o *testObservation) { o.BeforeTree = "a" }},
		{"before tree uppercase", "before_tree", func(o *testObservation) { o.BeforeTree = strings.Repeat("A", 40) }},
		{"before tree nonhex", "before_tree", func(o *testObservation) { o.BeforeTree = strings.Repeat("g", 40) }},
		{"before tree wrong length", "before_tree", func(o *testObservation) { o.BeforeTree = strings.Repeat("a", 63) }},
		{"after tree", "after_tree", func(o *testObservation) { o.AfterTree = "" }},
		{"before head", "before_head", func(o *testObservation) { o.BeforeHead = "" }},
		{"after head", "after_head", func(o *testObservation) { o.AfterHead = "" }},
		{"unreported source change", "source_changed", func(o *testObservation) { o.AfterTree = strings.Repeat("b", 40) }},
		{"spurious source change", "source_changed", func(o *testObservation) { o.SourceChanged = true }},
		{"unreported history change", "history_changed", func(o *testObservation) { o.AfterHead = strings.Repeat("b", 40) }},
		{"spurious history change", "history_changed", func(o *testObservation) { o.HistoryChanged = true }},
		{"unknown mode", "mode", func(o *testObservation) { o.Mode = "unknown" }},
		{"nil command", "command", func(o *testObservation) { o.Command = nil }},
		{"empty command", "command", func(o *testObservation) { o.Command = []string{} }},
		{"negative status", "exit_status", func(o *testObservation) { o.ExitStatus = -1 }},
		{"status above byte", "exit_status", func(o *testObservation) { o.ExitStatus = 256 }},
		{"signal below range", "signaled", func(o *testObservation) { o.Signaled, o.ExitStatus = true, 128 }},
		{"signal above range", "signaled", func(o *testObservation) { o.Signaled, o.ExitStatus = true, 193 }},
		{"launch below range", "launch_failed", func(o *testObservation) { o.LaunchFailed, o.ExitStatus = true, 125 }},
		{"launch above range", "launch_failed", func(o *testObservation) { o.LaunchFailed, o.ExitStatus = true, 128 }},
		{"empty timestamp", "observed_at", func(o *testObservation) { o.ObservedAt = "" }},
		{"invalid timestamp", "observed_at", func(o *testObservation) { o.ObservedAt = "yesterday" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := validationObservation()
			test.change(&o)
			if err := validateTestObservation(o); err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("validation error = %v, want field %q", err, test.field)
			}
		})
	}
}

func TestValidateTestObservationPreservesAcceptedShapes(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testObservation)
	}{
		{"baseline", func(o *testObservation) {}},
		{"full mode", func(o *testObservation) { o.Mode = testModeFull }},
		{"targeted mode", func(o *testObservation) { o.Mode = testModeTargeted }},
		{"whitespace worktree", func(o *testObservation) { o.Worktree = " " }},
		{"empty command element", func(o *testObservation) { o.Command = []string{""} }},
		{"64 character IDs", func(o *testObservation) {
			o.BeforeTree, o.AfterTree = strings.Repeat("a", 64), strings.Repeat("a", 64)
			o.BeforeHead, o.AfterHead = strings.Repeat("b", 64), strings.Repeat("b", 64)
		}},
		{"mixed ID lengths with consistent flags", func(o *testObservation) {
			o.AfterTree, o.AfterHead = strings.Repeat("b", 64), strings.Repeat("c", 64)
			o.SourceChanged, o.HistoryChanged = true, true
		}},
		{"fractional offset timestamp", func(o *testObservation) { o.ObservedAt = "2026-10-17T00:00:00.123456789+05:30" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			o := validationObservation()
			test.change(&o)
			if err := validateTestObservation(o); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateTestObservationPreservesStatusRanges(t *testing.T) {
	for status := 0; status <= 255; status++ {
		o := validationObservation()
		o.ExitStatus = status
		if err := validateTestObservation(o); err != nil {
			t.Fatalf("unflagged status %d: %v", status, err)
		}
		o.Signaled, o.LaunchFailed = true, true
		if err := validateTestObservation(o); err == nil {
			t.Fatalf("accepted contradictory signal/launch flags at status %d", status)
		}
	}
	for status := 129; status <= 192; status++ {
		o := validationObservation()
		o.Signaled, o.ExitStatus = true, status
		if err := validateTestObservation(o); err != nil {
			t.Fatalf("signal status %d: %v", status, err)
		}
	}
	for _, status := range []int{126, 127} {
		o := validationObservation()
		o.LaunchFailed, o.ExitStatus = true, status
		if err := validateTestObservation(o); err != nil {
			t.Fatalf("launch status %d: %v", status, err)
		}
	}
}

func TestValidateTestObservationPreservesUnsupportedSchemaDiagnostic(t *testing.T) {
	o := validationObservation()
	o.Schema = "unknown"
	if err := validateTestObservation(o); err == nil || err.Error() != `uses unsupported schema "unknown"` {
		t.Fatalf("schema error = %v", err)
	}
}

func TestLocalTestObservationsRetainsInvalidRecordContext(t *testing.T) {
	repository := newTestRunnerRepository(t)
	o := validationObservation()
	o.ExitStatus = 256
	if _, err := appendTestObservation(repository, o); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(runTestRunnerGit(t, repository, "rev-parse", testObservationRef))
	_, err := localTestObservations(repository)
	if err == nil || !strings.Contains(err.Error(), "local observation "+id) || !strings.Contains(err.Error(), "exit_status") {
		t.Fatalf("reader error = %v", err)
	}
}
