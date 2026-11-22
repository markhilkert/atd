package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func requireRealDirectories(repository, relative string) error {
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(filepath.Join(repository, current))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspecting %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s must be a non-symlink directory", current)
		}
	}
	return nil
}

// temporaryFilePrefix names writeFileAtomically's temporary files; a killed
// atd can leave one behind, so readers ignore them and writers remove them.
const temporaryFilePrefix = ".atd-"

func temporaryFile(entry os.DirEntry) bool {
	return strings.HasPrefix(entry.Name(), temporaryFilePrefix) && entry.Type().IsRegular()
}

func removeTemporaryFiles(directory string) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", directory, err)
	}
	for _, entry := range entries {
		if temporaryFile(entry) {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return fmt.Errorf("removing leftover %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

func writeFileAtomically(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, temporaryFilePrefix+"*")
	if err != nil {
		return fmt.Errorf("creating temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting permissions for %s: %w", path, err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("installing %s: %w", path, err)
	}
	return nil
}
