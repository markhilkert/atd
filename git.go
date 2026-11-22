package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func gitRoot() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("current directory is not an unambiguous Git repository")
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(output)))
	if err != nil {
		return "", fmt.Errorf("canonicalizing repository root: %w", err)
	}
	return root, nil
}

func gitEnvironment(overrides map[string]string) []string {
	environment := os.Environ()
	for key := range overrides {
		prefix := key + "="
		filtered := environment[:0]
		for _, entry := range environment {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		environment = filtered
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func gitOutput(repository string, environment []string, input []byte, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if environment != nil {
		command.Env = environment
	}
	if input != nil {
		command.Stdin = strings.NewReader(string(input))
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		diagnostics := strings.TrimSpace(strings.Join([]string{stderr.String(), stdout.String()}, "\n"))
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, diagnostics)
	}
	return stdout.String(), nil
}

// gitConfigValues returns every local value of key; an unset key has none.
func gitConfigValues(repository, key string) ([]string, error) {
	output, err := gitOutput(repository, nil, nil, "config", "--local", "--get-all", key)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", key, err)
	}
	return strings.Split(strings.TrimSuffix(output, "\n"), "\n"), nil
}
