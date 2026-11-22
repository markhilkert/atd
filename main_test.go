package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain hides the developer's Git configuration, including global ignore
// files that commonly exclude .claude/settings.local.json and would make
// visibility assertions vacuous.
func TestMain(m *testing.M) {
	configuration, err := os.MkdirTemp("", "atd-test-config-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for key, value := range map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1", "XDG_CONFIG_HOME": configuration} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(configuration)
	os.Exit(code)
}
