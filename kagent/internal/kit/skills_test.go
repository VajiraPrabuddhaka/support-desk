package kit

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestReadSkillsAndInstruction(t *testing.T) {
	fsys := fstest.MapFS{
		"triage-rubric/SKILL.md": {Data: []byte("---\nname: triage-rubric\ndescription: Labels and priorities.\n---\n\nUse type/* and priority/* labels.\n")},
		"plain/SKILL.md":         {Data: []byte("No frontmatter here.\n")},
		"empty-folder/notes.txt": {Data: []byte("not a skill")},
		".hidden/SKILL.md":       {Data: []byte("ignored")},
	}
	skills, err := readSkills(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0].name != "plain" || skills[1].name != "triage-rubric" {
		t.Fatalf("skills = %+v", skills)
	}
	if skills[1].description != "Labels and priorities." || skills[1].body != "Use type/* and priority/* labels." {
		t.Errorf("triage-rubric = %+v", skills[1])
	}

	got := withSkills("  You triage tickets.  ", skills)
	for _, want := range []string{"You triage tickets.\n", "# Skills", "## Skill: plain", "## Skill: triage-rubric", "Labels and priorities."} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction lacks %q:\n%s", want, got)
		}
	}
	if withSkills("Only a prompt.", nil) != "Only a prompt.\n" {
		t.Errorf("a prompt without skills changed: %q", withSkills("Only a prompt.", nil))
	}
}

func TestReadSkillsWithoutFolder(t *testing.T) {
	skills, err := readSkills(fstest.MapFS{})
	if err != nil || len(skills) != 0 {
		t.Fatalf("skills = %+v, err = %v", skills, err)
	}
}

func TestParseSkillRejectsAnEmptyBody(t *testing.T) {
	if _, err := parseSkill("x", "---\nname: x\n---\n"); err == nil {
		t.Error("a skill without instructions was accepted")
	}
	if _, err := parseSkill("x", "---\nname: x\n"); err == nil {
		t.Error("an unclosed frontmatter was accepted")
	}
}
