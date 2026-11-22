package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewerInstructionsAreReachableAfterEnableAndUpdate(t *testing.T) {
	current := embeddedSkills
	for _, harness := range harnessesForTest {
		for _, operation := range []string{"enable", "update"} {
			t.Run(harness.name+"/"+operation, func(t *testing.T) {
				repository := newTestRunnerRepository(t)
				installFakeATDForTest(t)
				withTestRunnerDirectory(t, repository)
				if operation == "update" {
					useEmbeddedSkillsForTest(t, previousReleaseSkillsForTest())
					if err := runCLI([]string{"enable", harness.name}, io.Discard); err != nil {
						t.Fatal(err)
					}
				}
				useEmbeddedSkillsForTest(t, current)
				arguments := []string{"enable", harness.name}
				if operation == "update" {
					arguments = []string{"update"}
				}
				if err := runCLI(arguments, io.Discard); err != nil {
					t.Fatal(err)
				}

				root := filepath.Join(repository, harness.skillsRoot)
				reviewerPath := filepath.Join(root, "atd-reviewer", "SKILL.md")
				if _, err := os.ReadFile(reviewerPath); err != nil {
					t.Fatalf("reviewer cannot load its installed instructions: %v", err)
				}
				coordinator, err := os.ReadFile(filepath.Join(root, "atd-review", "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(coordinator), "](../atd-reviewer/SKILL.md)") {
					t.Fatal("coordinator has no installed link to the reviewer instructions")
				}
				designReviewerPath := filepath.Join(root, "atd-design-review", "SKILL.md")
				if _, err := os.ReadFile(designReviewerPath); err != nil {
					t.Fatalf("design reviewer cannot load its installed instructions: %v", err)
				}
				tdd, err := os.ReadFile(filepath.Join(root, "atd-tdd", "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(tdd), "](../atd-design-review/SKILL.md)") {
					t.Fatal("TDD has no installed link to the design reviewer instructions")
				}
				investigate, err := os.ReadFile(filepath.Join(root, "atd-investigate", "SKILL.md"))
				if err != nil {
					t.Fatalf("coordinator cannot load the installed change-authority guidance: %v", err)
				}
				if !strings.Contains(string(investigate), "## Change authority") {
					t.Fatal("installed investigation skill has no change-authority anchor")
				}
				if !strings.Contains(string(tdd), "](../atd-investigate/SKILL.md#change-authority)") {
					t.Fatal("TDD has no installed link to change-authority guidance")
				}
				designReviewer, err := os.ReadFile(designReviewerPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(designReviewer), "](../atd-investigate/SKILL.md#change-authority)") {
					t.Fatal("design review has no installed link to change-authority guidance")
				}
				featureContract, err := os.ReadFile(filepath.Join(root, "atd-feature-contract", "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(featureContract), "](../atd-investigate/SKILL.md#change-authority)") {
					t.Fatal("feature contract has no installed link to change-authority guidance")
				}
				_, designStage, found := strings.Cut(string(tdd), "## Separate design review")
				if !found || !strings.Contains(designStage, "](../atd-feature-contract/SKILL.md)") {
					t.Fatal("design-review handoff has no installed link to the active feature-contract guidance")
				}
				assertInstalledSkillsForTest(t, harness, repository, embeddedSourceSkillsForTest(t))
				runTestRunnerGit(t, repository, "check-ignore", "-q", "--no-index", "--", reviewerPath)
				runTestRunnerGit(t, repository, "check-ignore", "-q", "--no-index", "--", designReviewerPath)
			})
		}
	}
}
