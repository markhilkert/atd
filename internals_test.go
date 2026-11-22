package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var updateInternalsCitations = flag.Bool("update", false, "rewrite the Go citation anchors in docs/internals.md to the current declaration spans")

var internalsDocument = filepath.Join("docs", "internals.md")

// TestInternalsCitations keeps every citation in the internals document
// pointing at what it names in this tree; -update refreshes Go line anchors.
func TestInternalsCitations(t *testing.T) {
	if testing.Short() {
		t.Skip("the full suite checks internals citations")
	}
	contents, err := os.ReadFile(internalsDocument)
	if err != nil {
		t.Fatal(err)
	}
	if *updateInternalsCitations {
		updated := internalsCitationsUpdated(internalsDocument, contents)
		if !bytes.Equal(updated, contents) {
			if err := os.WriteFile(internalsDocument, updated, 0o644); err != nil {
				t.Fatal(err)
			}
			contents = updated
		}
	}
	for _, problem := range internalsCitationProblems(internalsDocument, contents) {
		t.Error(problem)
	}
}

var (
	internalsLinkDefinition   = regexp.MustCompile(`^ {0,3}\[([^\]]+)\]:[ \t]+(\S+)`)
	internalsDeclarationLabel = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	internalsLineRangeLabel   = regexp.MustCompile(`:\d+(-\d+)?$`)
	internalsExternalTarget   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	internalsPinnedLink       = regexp.MustCompile(`github\.com/markhilkert/atd/(blob|tree)\b`)
	markdownATXHeading        = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
)

const internalsPinnedLinkProblem = "links into github.com/markhilkert/atd/blob or /tree, which pins a commit; use a relative link"

// internalsCitation is one reference-style link definition, [label]: target.
type internalsCitation struct {
	line                   int
	label, target          string
	targetStart, targetEnd int
}

// internalsCitations lists the link definitions outside fenced code blocks.
// As in CommonMark, a definition cannot continue a paragraph, so a wrapped
// line that happens to start with "[label]: " is prose.
func internalsCitations(contents []byte) []internalsCitation {
	var citations []internalsCitation
	lines := strings.Split(string(contents), "\n")
	mayDefine := true
	for index, fenced := range markdownFencedLines(lines) {
		if fenced {
			mayDefine = true
			continue
		}
		match := internalsLinkDefinition.FindStringSubmatchIndex(lines[index])
		if match == nil || !mayDefine {
			trimmed := strings.TrimSpace(lines[index])
			mayDefine = trimmed == "" || strings.HasSuffix(trimmed, "-->") || markdownATXHeading.MatchString(lines[index])
			continue
		}
		citations = append(citations, internalsCitation{
			line:        index + 1,
			label:       lines[index][match[2]:match[3]],
			target:      lines[index][match[4]:match[5]],
			targetStart: match[4],
			targetEnd:   match[5],
		})
	}
	return citations
}

func markdownFencedLines(lines []string) []bool {
	fenced := make([]bool, len(lines))
	fence := ""
	for index, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if fence == "" {
			for _, character := range []string{"`", "~"} {
				if length := len(trimmed) - len(strings.TrimLeft(trimmed, character)); length >= 3 {
					fence = strings.Repeat(character, length)
					fenced[index] = true
				}
			}
			continue
		}
		fenced[index] = true
		if strings.HasPrefix(trimmed, fence) && strings.TrimRight(trimmed, fence[:1]+" \t") == "" {
			fence = ""
		}
	}
	return fenced
}

// internalsCitationChecker resolves the citations of one document, relative to
// the document's directory, caching what it parses.
type internalsCitationChecker struct {
	document     string
	declarations map[string]map[string]string
	slugs        map[string]map[string]bool
}

func newInternalsCitationChecker(document string) *internalsCitationChecker {
	return &internalsCitationChecker{document: document, declarations: map[string]map[string]string{}, slugs: map[string]map[string]bool{}}
}

// internalsCitationProblems checks every citation in the document and every
// line for links that pin a commit.
func internalsCitationProblems(document string, contents []byte) []string {
	checker := newInternalsCitationChecker(document)
	definitions := map[int]internalsCitation{}
	for _, citation := range internalsCitations(contents) {
		definitions[citation.line] = citation
	}
	var problems []string
	for index, line := range strings.Split(string(contents), "\n") {
		where := fmt.Sprintf("%s:%d", document, index+1)
		citation, defined := definitions[index+1]
		switch {
		case defined:
			if problem := checker.problem(citation); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: [%s] %s", where, citation.label, problem))
			}
		case internalsPinnedLink.MatchString(line):
			problems = append(problems, where+": "+internalsPinnedLinkProblem)
		}
	}
	return problems
}

// internalsCitationsUpdated rewrites the line anchor of each Go citation whose
// declaration it finds to that declaration's current span, leaving every other
// byte unchanged.
func internalsCitationsUpdated(document string, contents []byte) []byte {
	checker := newInternalsCitationChecker(document)
	lines := strings.Split(string(contents), "\n")
	for _, citation := range internalsCitations(contents) {
		path, _, _ := strings.Cut(citation.target, "#")
		anchor, err := checker.goAnchor(citation)
		if err != nil || anchor == "" {
			continue
		}
		line := lines[citation.line-1]
		lines[citation.line-1] = line[:citation.targetStart] + path + "#" + anchor + line[citation.targetEnd:]
	}
	return []byte(strings.Join(lines, "\n"))
}

func (checker *internalsCitationChecker) problem(citation internalsCitation) string {
	if internalsPinnedLink.MatchString(citation.target) {
		return internalsPinnedLinkProblem
	}
	if internalsExternalTarget.MatchString(citation.target) {
		return ""
	}
	path, fragment, hasFragment := strings.Cut(citation.target, "#")
	switch {
	case strings.HasSuffix(path, ".go"):
		anchor, err := checker.goAnchor(citation)
		if err != nil {
			return err.Error()
		}
		if !hasFragment || fragment != anchor {
			cited := "no line anchor"
			if hasFragment {
				cited = "#" + fragment
			}
			return fmt.Sprintf("cites %s, but the declaration in %s spans #%s; run go test -run TestInternalsCitations -update", cited, path, anchor)
		}
	case hasFragment && (path == "" || strings.HasSuffix(path, ".md")):
		slugs, err := checker.headingSlugs(checker.resolve(path))
		if err != nil {
			return err.Error()
		}
		if !slugs[fragment] {
			return fmt.Sprintf("cites #%s, but %s has no heading with that slug", fragment, citation.target[:len(path)])
		}
	default:
		if _, err := os.Stat(checker.resolve(path)); err != nil {
			return fmt.Sprintf("cites %s, which does not exist", path)
		}
	}
	return ""
}

func (checker *internalsCitationChecker) resolve(path string) string {
	if path == "" {
		return checker.document
	}
	return filepath.Join(filepath.Dir(checker.document), filepath.FromSlash(path))
}

// goAnchor returns the #L anchor of the declaration a Go citation names, or
// an error when the citation cannot name one.
func (checker *internalsCitationChecker) goAnchor(citation internalsCitation) (string, error) {
	path, _, _ := strings.Cut(citation.target, "#")
	if internalsExternalTarget.MatchString(path) || !strings.HasSuffix(path, ".go") {
		return "", nil
	}
	name := strings.TrimSuffix(strings.TrimPrefix(citation.label, "`"), "`")
	if internalsLineRangeLabel.MatchString(citation.label) {
		return "", errors.New("names a line range; cite the enclosing declaration as [`Name`] or [`Type.Method`]")
	}
	if !internalsDeclarationLabel.MatchString(name) {
		return "", errors.New("must name a Go declaration as [`Name`] or [`Type.Method`]")
	}
	declarations, err := checker.goDeclarations(checker.resolve(path))
	if err != nil {
		return "", err
	}
	anchor, found := declarations[name]
	if !found {
		return "", fmt.Errorf("names no declaration in %s; %s is gone or moved", path, name)
	}
	return anchor, nil
}

// goDeclarations maps each top-level name, and Type.Method for methods, to the
// #L anchor of its span: from the line of its func, type or name token to its
// last line, without doc comments. A grouped const, var or type spans only its
// own spec.
func (checker *internalsCitationChecker) goDeclarations(path string) (map[string]string, error) {
	if declarations, parsed := checker.declarations[path]; parsed {
		return declarations, nil
	}
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	anchor := func(from, to token.Pos) string {
		start, end := files.Position(from).Line, files.Position(to).Line
		if start == end {
			return fmt.Sprintf("L%d", start)
		}
		return fmt.Sprintf("L%d-L%d", start, end)
	}
	declarations := map[string]string{}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			name := declaration.Name.Name
			if declaration.Recv != nil && len(declaration.Recv.List) == 1 {
				name = goReceiverTypeName(declaration.Recv.List[0].Type) + "." + name
			}
			declarations[name] = anchor(declaration.Pos(), declaration.End())
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				from, to := spec.Pos(), spec.End()
				if !declaration.Lparen.IsValid() {
					from, to = declaration.Pos(), declaration.End()
				}
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					declarations[spec.Name.Name] = anchor(from, to)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						declarations[name.Name] = anchor(from, to)
					}
				}
			}
		}
	}
	checker.declarations[path] = declarations
	return declarations, nil
}

func goReceiverTypeName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.StarExpr:
		return goReceiverTypeName(expression.X)
	case *ast.ParenExpr:
		return goReceiverTypeName(expression.X)
	case *ast.IndexExpr:
		return goReceiverTypeName(expression.X)
	case *ast.IndexListExpr:
		return goReceiverTypeName(expression.X)
	case *ast.Ident:
		return expression.Name
	}
	return ""
}

// headingSlugs returns the anchors GitHub gives a Markdown file's ATX
// headings outside fenced code blocks.
func (checker *internalsCitationChecker) headingSlugs(path string) (map[string]bool, error) {
	if slugs, parsed := checker.slugs[path]; parsed {
		return slugs, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	slugs := map[string]bool{}
	occurrences := map[string]int{}
	lines := strings.Split(string(contents), "\n")
	for index, fenced := range markdownFencedLines(lines) {
		heading := markdownATXHeading.FindStringSubmatch(lines[index])
		if fenced || heading == nil {
			continue
		}
		base := githubHeadingSlug(heading[1])
		slug := base
		for slugs[slug] {
			occurrences[base]++
			slug = fmt.Sprintf("%s-%d", base, occurrences[base])
		}
		slugs[slug] = true
	}
	checker.slugs[path] = slugs
	return slugs, nil
}

// githubHeadingSlug lowercases a heading, drops punctuation other than
// hyphens and underscores, and turns each space into a hyphen.
func githubHeadingSlug(heading string) string {
	var slug strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case character == ' ':
			slug.WriteRune('-')
		case character == '-' || character == '_' || unicode.IsLetter(character) || unicode.IsNumber(character):
			slug.WriteRune(character)
		}
	}
	return slug.String()
}

// internalsFixtureGo is parsed, not compiled. Its spans: documented 4-6, after
// its doc comment; widget 8-10; widget.spin 12; widget.stop 14-16; single 18;
// first 21 and second 23 inside their group; grouped 27-29; TestFixture 32-33.
var internalsFixtureGo = `package fixture

// documented has a doc comment citations omit.
func documented() {
	_ = 1
}

type widget struct {
	size int
}

func (w *widget) spin() {}

func (w widget) stop() {
	_ = w
}

const single = 1

const (
	first = 1
	// second is documented inside its group.
	second = 2
)

var (
	grouped = map[string]int{
		"a": 1,
	}
)

func TestFixture(t *testing.T) {
}
`

var internalsFixtureMarkdown = strings.Join([]string{
	"# Guide",
	"",
	"## Set up: the `tool`, quickly",
	"",
	"## Notes",
	"",
	"## Notes",
	"",
	"```sh",
	"# not a heading",
	"```",
}, "\n") + "\n"

// internalsCitationFixtureForTest writes the fixture sources beside a
// docs/internals.md holding the given link definitions and returns its path.
func internalsCitationFixtureForTest(t *testing.T, definitions ...string) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	writeFileForTest(t, filepath.Join(root, "fixture.go"), []byte(internalsFixtureGo))
	writeFileForTest(t, filepath.Join(root, "GUIDE.md"), []byte(internalsFixtureMarkdown))
	writeFileForTest(t, filepath.Join(root, "script", "test"), []byte("#!/bin/sh\n"))
	contents := []byte("# Internals\n\nSee [`documented`].\n\n" + strings.Join(definitions, "\n") + "\n")
	document := filepath.Join(root, "docs", "internals.md")
	writeFileForTest(t, document, contents)
	return document, contents
}

func assertNoInternalsCitationProblemsForTest(t *testing.T, definitions ...string) {
	t.Helper()
	document, contents := internalsCitationFixtureForTest(t, definitions...)
	if problems := internalsCitationProblems(document, contents); len(problems) > 0 {
		t.Fatalf("unexpected problems:\n%s", strings.Join(problems, "\n"))
	}
}

// assertInternalsCitationProblemForTest requires exactly one problem, naming
// each of the given parts.
func assertInternalsCitationProblemForTest(t *testing.T, definition string, parts ...string) {
	t.Helper()
	document, contents := internalsCitationFixtureForTest(t, definition)
	problems := internalsCitationProblems(document, contents)
	if len(problems) != 1 {
		t.Fatalf("%s: problems = %q, want one", definition, problems)
	}
	for _, part := range parts {
		if !strings.Contains(problems[0], part) {
			t.Fatalf("%s: problem %q does not name %q", definition, problems[0], part)
		}
	}
}

func TestInternalsCitationCheckerAcceptsCurrentDeclarationSpans(t *testing.T) {
	assertNoInternalsCitationProblemsForTest(t,
		"[`documented`]: ../fixture.go#L4-L6",
		"[`widget`]: ../fixture.go#L8-L10",
		"[`single`]: ../fixture.go#L18",
		"[`second`]: ../fixture.go#L23",
		"[`grouped`]: ../fixture.go#L27-L29",
		"[`TestFixture`]: ../fixture.go#L32-L33",
		"[script/test]: ../script/test",
		"[GUIDE.md]: ../GUIDE.md",
		"[Go]: https://go.dev/ref/spec",
	)
}

func TestInternalsCitationCheckerResolvesMethodLabels(t *testing.T) {
	assertNoInternalsCitationProblemsForTest(t,
		"[`widget.spin`]: ../fixture.go#L12",
		"[`widget.stop`]: ../fixture.go#L14-L16",
	)
	assertInternalsCitationProblemForTest(t, "[`gadget.spin`]: ../fixture.go#L12", "[`gadget.spin`]", "gadget.spin")
	assertInternalsCitationProblemForTest(t, "[`spin`]: ../fixture.go#L12", "[`spin`]")
}

func TestInternalsCitationCheckerReportsStaleAnchor(t *testing.T) {
	assertInternalsCitationProblemForTest(t, "[`documented`]: ../fixture.go#L3-L6", "[`documented`]", "#L3-L6", "#L4-L6")
	assertInternalsCitationProblemForTest(t, "[`widget.spin`]: ../fixture.go#L12-L13", "[`widget.spin`]", "#L12-L13", "#L12")
	assertInternalsCitationProblemForTest(t, "[`second`]: ../fixture.go#L20-L24", "[`second`]", "#L20-L24", "#L23")
	assertInternalsCitationProblemForTest(t, "[`widget`]: ../fixture.go", "[`widget`]", "#L8-L10")
}

func TestInternalsCitationCheckerReportsMissingDeclaration(t *testing.T) {
	assertInternalsCitationProblemForTest(t, "[`vanished`]: ../fixture.go#L4-L6", "[`vanished`]", "vanished")
	assertInternalsCitationProblemForTest(t, "[`documented`]: ../moved.go#L4-L6", "[`documented`]", "moved.go")
}

func TestInternalsCitationCheckerRejectsLineRangeLabels(t *testing.T) {
	assertInternalsCitationProblemForTest(t, "[fixture.go:4-6]: ../fixture.go#L4-L6", "[fixture.go:4-6]")
	assertInternalsCitationProblemForTest(t, "[fixture.go:12]: ../fixture.go#L12", "[fixture.go:12]")
}

func TestInternalsCitationCheckerRequiresMarkdownHeadingSlug(t *testing.T) {
	assertNoInternalsCitationProblemsForTest(t,
		"[GUIDE.md#guide]: ../GUIDE.md#guide",
		"[GUIDE.md#set-up-the-tool-quickly]: ../GUIDE.md#set-up-the-tool-quickly",
		"[GUIDE.md#notes]: ../GUIDE.md#notes",
		"[GUIDE.md#notes-1]: ../GUIDE.md#notes-1",
		"[this document]: #internals",
	)
	for _, slug := range []string{"missing", "notes-2", "not-a-heading", "L3-L5"} {
		assertInternalsCitationProblemForTest(t, "[GUIDE]: ../GUIDE.md#"+slug, "[GUIDE]", "#"+slug)
	}
	assertInternalsCitationProblemForTest(t, "[MISSING.md#guide]: ../MISSING.md#guide", "[MISSING.md#guide]", "MISSING.md")
}

func TestInternalsCitationCheckerRequiresExistingRelativeFiles(t *testing.T) {
	assertInternalsCitationProblemForTest(t, "[script/test_fast]: ../script/test_fast", "[script/test_fast]", "test_fast")
}

func TestInternalsCitationCheckerRejectsPinnedCommitLinks(t *testing.T) {
	assertNoInternalsCitationProblemsForTest(t, "[atd]: https://github.com/markhilkert/atd")
	for _, target := range []string{
		"https://github.com/markhilkert/atd/blob/0123456789abcdef0123456789abcdef01234567/main.go#L12-L21",
		"https://github.com/markhilkert/atd/tree/0123456789abcdef0123456789abcdef01234567",
	} {
		assertInternalsCitationProblemForTest(t, "[pinned]: "+target, "[pinned]")
	}
	document, contents := internalsCitationFixtureForTest(t)
	contents = append(contents, "Checked at [`0123456`](https://github.com/markhilkert/atd/tree/0123456).\n"...)
	if problems := internalsCitationProblems(document, contents); len(problems) != 1 || !strings.Contains(problems[0], "internals.md:6") {
		t.Fatalf("inline pinned link problems = %q, want one naming line 6", problems)
	}
}

func TestInternalsCitationCheckerUpdateRewritesOnlyGoAnchors(t *testing.T) {
	document, contents := internalsCitationFixtureForTest(t,
		"[`documented`]: ../fixture.go#L1-L2",
		"[`widget.spin`]: ../fixture.go",
		"[`second`]: ../fixture.go#L20-L24 \"grouped constant\"",
		"[`vanished`]: ../fixture.go#L9",
		"[fixture.go:4-6]: ../fixture.go#L1",
		"[GUIDE]: ../GUIDE.md#missing",
		"[pinned]: https://github.com/markhilkert/atd/blob/0123456/fixture.go#L1",
		"",
		"```text",
		"[`widget`]: ../fixture.go#L1",
		"```",
	)
	updated := internalsCitationsUpdated(document, contents)
	want := strings.NewReplacer(
		"[`documented`]: ../fixture.go#L1-L2", "[`documented`]: ../fixture.go#L4-L6",
		"[`widget.spin`]: ../fixture.go\n", "[`widget.spin`]: ../fixture.go#L12\n",
		"[`second`]: ../fixture.go#L20-L24 ", "[`second`]: ../fixture.go#L23 ",
	).Replace(string(contents))
	if string(updated) != want {
		t.Fatalf("updated document:\n%s\nwant:\n%s", updated, want)
	}
	problems := internalsCitationProblems(document, updated)
	if len(problems) != 4 {
		t.Fatalf("problems after update = %q, want the four update cannot fix", problems)
	}
	for index, label := range []string{"[`vanished`]", "[fixture.go:4-6]", "[GUIDE]", "[pinned]"} {
		if !strings.Contains(problems[index], label) {
			t.Fatalf("problem %d = %q, want it to name %s", index, problems[index], label)
		}
	}
}

func TestInternalsCitationCheckerIgnoresParagraphContinuationLines(t *testing.T) {
	document, contents := internalsCitationFixtureForTest(t,
		"A paragraph that wraps onto a line starting with",
		"[`widget`]: a phrase, not a definition.",
		"",
		"<!-- definitions -->",
		"[`documented`]: ../fixture.go#L3-L6",
	)
	problems := internalsCitationProblems(document, contents)
	if len(problems) != 1 || !strings.Contains(problems[0], "[`documented`]") {
		t.Fatalf("problems = %q, want only the stale definition after the comment", problems)
	}
}
