package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed .agents/skills
var embeddedSkillFiles embed.FS

var embeddedSkills = mustSubFS(embeddedSkillFiles, ".agents/skills")

const (
	installedSkillPrefix = "atd-"
	skillFile            = "SKILL.md"
	skillOwnershipPrefix = "<!-- atd-managed sha256:"
	skillOwnershipSuffix = " -->"
)

var (
	markdownLink   = regexp.MustCompile(`\]\(([^)]*)\)`)
	siblingSkill   = regexp.MustCompile(`^\.\./([a-z0-9-]+)/SKILL\.md(#.*)?$`)
	externalTarget = regexp.MustCompile(`^([a-z]+:|#)`)
)

func mustSubFS(files fs.FS, directory string) fs.FS {
	sub, err := fs.Sub(files, directory)
	if err != nil {
		panic(err)
	}
	return sub
}

// renderSkills namespaces each embedded skill as atd-<name>, points its
// cross-skill links at the namespaced copies, and appends an ownership marker
// whose hash lets update tell untouched copies from hand-edited ones.
func renderSkills() (map[string][]byte, error) {
	entries, err := fs.ReadDir(embeddedSkills, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded skills: %w", err)
	}
	// Codex reads installed copies from the same directory as the source, so
	// an enabled ATD checkout holds atd-* copies next to the skills they render.
	var names []string
	isSkill := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), installedSkillPrefix) {
			names = append(names, entry.Name())
			isSkill[entry.Name()] = true
		}
	}
	rendered := map[string][]byte{}
	for _, name := range names {
		files, err := fs.ReadDir(embeddedSkills, name)
		if err != nil || len(files) != 1 || files[0].Name() != skillFile {
			return nil, fmt.Errorf("embedded skill %s must contain only %s", name, skillFile)
		}
		source, err := fs.ReadFile(embeddedSkills, name+"/"+skillFile)
		if err != nil {
			return nil, fmt.Errorf("reading embedded skill %s: %w", name, err)
		}
		body, err := renderSkill(name, string(source), isSkill)
		if err != nil {
			return nil, err
		}
		rendered[installedSkillPrefix+name] = withSkillOwnership([]byte(body))
	}
	return rendered, nil
}

func renderSkill(name, source string, names map[string]bool) (string, error) {
	nameLine := "\nname: " + name + "\n"
	rest, opened := strings.CutPrefix(source, "---\n")
	frontMatter, _, closed := strings.Cut(rest, "\n---\n")
	if !opened || !closed || !strings.Contains("\n"+frontMatter+"\n", nameLine) {
		return "", fmt.Errorf("embedded skill %s must declare name: %s in its front matter", name, name)
	}
	body := strings.Replace(source, nameLine, "\nname: "+installedSkillPrefix+name+"\n", 1)
	var linkErr error
	body = markdownLink.ReplaceAllStringFunc(body, func(link string) string {
		target := markdownLink.FindStringSubmatch(link)[1]
		if externalTarget.MatchString(target) {
			return link
		}
		sibling := siblingSkill.FindStringSubmatch(target)
		if sibling == nil || !names[sibling[1]] {
			linkErr = fmt.Errorf("embedded skill %s links outside the installed skills: %s", name, target)
			return link
		}
		return "](../" + installedSkillPrefix + strings.TrimPrefix(target, "../") + ")"
	})
	if linkErr != nil {
		return "", linkErr
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body, nil
}

func withSkillOwnership(body []byte) []byte {
	digest := sha256.Sum256(body)
	return append(append([]byte{}, body...), skillOwnershipPrefix+hex.EncodeToString(digest[:])+skillOwnershipSuffix+"\n"...)
}

func skillOwnershipIntact(contents []byte) bool {
	trimmed := bytes.TrimSuffix(contents, []byte("\n"))
	index := bytes.LastIndexByte(trimmed, '\n')
	if index < 0 || len(trimmed) == len(contents) {
		return false
	}
	marker := string(trimmed[index+1:])
	if !strings.HasPrefix(marker, skillOwnershipPrefix) || !strings.HasSuffix(marker, skillOwnershipSuffix) {
		return false
	}
	body := contents[:index+1]
	return bytes.Equal(withSkillOwnership(body), contents)
}

type skillState string

const (
	skillCurrent skillState = "current"
	skillStale   skillState = "stale"
	skillMissing skillState = "missing"
	skillRetired skillState = "retired"
	skillEdited  skillState = "edited"
)

type installedSkill struct {
	name  string
	state skillState
	// path names the file or directory a refusal must point the developer at.
	path string
}

// inspectSkills classifies every atd-* entry in the harness skills root
// against the rendered skills; everything else in the root is host-owned.
func inspectSkills(repository string, h harness, rendered map[string][]byte) ([]installedSkill, error) {
	if err := requireRealDirectories(repository, h.skillsRoot); err != nil {
		return nil, err
	}
	root := filepath.Join(repository, h.skillsRoot)
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading %s: %w", h.skillsRoot, err)
	}
	var skills []installedSkill
	present := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, installedSkillPrefix) {
			continue
		}
		present[name] = true
		skill := installedSkill{name: name, path: filepath.Join(h.skillsRoot, name, skillFile)}
		contents, owned := readOwnedSkill(filepath.Join(root, name))
		switch {
		case !owned:
			skill.state = skillEdited
			if !entry.IsDir() {
				skill.path = filepath.Join(h.skillsRoot, name)
			}
		case rendered[name] == nil:
			skill.state = skillRetired
		case contents == nil:
			skill.state = skillMissing
		case bytes.Equal(contents, rendered[name]):
			skill.state = skillCurrent
		default:
			skill.state = skillStale
		}
		skills = append(skills, skill)
	}
	for name := range rendered {
		if !present[name] {
			skills = append(skills, installedSkill{name: name, state: skillMissing, path: filepath.Join(h.skillsRoot, name, skillFile)})
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].name < skills[j].name })
	return skills, nil
}

// readOwnedSkill reports whether atd owns an installed skill directory. A
// directory that is empty or holds only temporary files is atd's interrupted
// first write: owned, with no contents.
func readOwnedSkill(directory string) ([]byte, bool) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		return nil, false
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, false
	}
	var files []os.DirEntry
	for _, entry := range entries {
		if !temporaryFile(entry) {
			files = append(files, entry)
		}
	}
	if len(files) == 0 {
		return nil, true
	}
	if len(files) != 1 || files[0].Name() != skillFile || !files[0].Type().IsRegular() {
		return nil, false
	}
	contents, err := os.ReadFile(filepath.Join(directory, skillFile))
	if err != nil || !skillOwnershipIntact(contents) {
		return nil, false
	}
	return contents, true
}
