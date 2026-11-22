package main

import (
	"errors"
	"fmt"
)

const repositoryScopeConfigKey = "atd.scope"

func parseInitScope(arguments []string) (string, bool, error) {
	if len(arguments) == 0 {
		return "", false, nil
	}
	if len(arguments) != 2 || arguments[0] != "--scope" {
		return "", false, errors.New("usage: atd init [--scope owner-controlled|ticket-bounded]")
	}
	if !validRepositoryScope(arguments[1]) {
		return "", false, fmt.Errorf("unknown scope %q; choose owner-controlled or ticket-bounded", arguments[1])
	}
	return arguments[1], true, nil
}

func validRepositoryScope(scope string) bool {
	switch scope {
	case "owner-controlled", "ticket-bounded":
		return true
	default:
		return false
	}
}

func setRepositoryScope(repository, scope string) error {
	if _, err := gitOutput(repository, nil, nil, "config", "--local", "--replace-all", repositoryScopeConfigKey, scope); err != nil {
		return fmt.Errorf("recording %s: %w", repositoryScopeConfigKey, err)
	}
	return nil
}

func repositoryScopeStatus(repository string) (string, error) {
	values, err := gitConfigValues(repository, repositoryScopeConfigKey)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "scope: unconfigured", nil
	}
	if len(values) == 1 && validRepositoryScope(values[0]) {
		return "scope: " + values[0], nil
	}
	return "scope: invalid; repair with atd init --scope owner-controlled or atd init --scope ticket-bounded", nil
}
