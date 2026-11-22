package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := runCLI(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "atd:", err)
		var child interface{ ExitStatus() int }
		if errors.As(err, &child) {
			os.Exit(child.ExitStatus())
		}
		os.Exit(1)
	}
}

func runCLI(arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("usage: atd init | enable | disable | update | status | version | test | verify | hook")
	}
	if arguments[0] == "version" {
		switch {
		case len(arguments) == 1:
			return writeVersion(output)
		case len(arguments) == 2 && arguments[1] == "--check":
			return checkVersion(output)
		}
		return errors.New("usage: atd version [--check]")
	}
	if arguments[0] == "hook" {
		if len(arguments) != 2 || (arguments[1] != string(steeringTriggerEdit) && arguments[1] != string(steeringTriggerCommand)) {
			return errors.New("usage: atd hook edit | command")
		}
		repository, err := gitRoot()
		if err != nil {
			return nil
		}
		return runSteeringHook(repository, steeringTrigger(arguments[1]), output)
	}
	if arguments[0] == "_claude-hook" || arguments[0] == "_codex-hook" {
		if len(arguments) != 2 || (arguments[1] != string(steeringTriggerEdit) && arguments[1] != string(steeringTriggerCommand)) {
			return errors.New("invalid internal hook invocation")
		}
		repository, err := gitRoot()
		if err != nil {
			return nil
		}
		var context bytes.Buffer
		if err := runSteeringHook(repository, steeringTrigger(arguments[1]), &context); err != nil {
			return err
		}
		if context.Len() == 0 {
			return nil
		}
		return writeHookContext(output, strings.TrimSuffix(context.String(), "\n"))
	}
	repository, err := gitRoot()
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "init":
		scope, setScope, err := parseInitScope(arguments[1:])
		if err != nil {
			return err
		}
		if err := initializeTestScripts(repository); err != nil {
			return err
		}
		if setScope {
			if err := setRepositoryScope(repository, scope); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(output, "initialized test scripts")
		return err
	case "enable":
		if len(arguments) != 2 {
			return errors.New("usage: atd enable claude | codex")
		}
		h, ok := harnessNamed(arguments[1])
		if !ok {
			return fmt.Errorf("unknown harness %q; usage: atd enable claude | codex", arguments[1])
		}
		return enableHarness(repository, h, output)
	case "disable":
		h, force, err := parseDisableCommand(arguments[1:])
		if err != nil {
			return err
		}
		return disableHarness(repository, h, force, output)
	case "update":
		if len(arguments) != 1 {
			return errors.New("usage: atd update")
		}
		return updateHarnesses(repository, output)
	case "status":
		if len(arguments) != 1 {
			return errors.New("usage: atd status")
		}
		return writeStatus(repository, output)
	case "test":
		mode, command, err := parseTestCommand(arguments[1:])
		if err != nil {
			return err
		}
		return runTestCommandWithMode(repository, mode, command, os.Stdin, output, os.Stderr)
	case "verify":
		base, candidate, err := parseVerifyCommand(arguments[1:])
		if err != nil {
			return err
		}
		return verifyCandidate(repository, base, candidate, output)
	}
	return fmt.Errorf("unknown command %q", arguments[0])
}
