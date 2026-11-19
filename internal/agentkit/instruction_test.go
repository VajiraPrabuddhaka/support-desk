package agentkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseSkill(t *testing.T) {
	tests := []struct {
		name, text      string
		wantName        string
		wantDescription string
		wantBody        string
		wantErr         bool
	}{
		{
			name:            "frontmatter and body",
			text:            "---\nname: billing-policy\ndescription: How to bill.\n---\n\n# Policy\n\nVerify first.\n",
			wantName:        "billing-policy",
			wantDescription: "How to bill.",
			wantBody:        "# Policy\n\nVerify first.",
		},
		{
			name:     "no frontmatter",
			text:     "Just rules.\n",
			wantName: "folder",
			wantBody: "Just rules.",
		},
		{
			name:     "windows line endings",
			text:     "---\r\nname: x\r\n---\r\nBody\r\n",
			wantName: "x",
			wantBody: "Body",
		},
		{name: "unclosed frontmatter", text: "---\nname: x\n", wantErr: true},
		{name: "invalid yaml", text: "---\nname: [x\n---\nBody\n", wantErr: true},
		{name: "empty body", text: "---\nname: x\n---\n\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parseSkill("folder", tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.name != tt.wantName || s.description != tt.wantDescription || s.body != tt.wantBody {
				t.Fatalf("got %+v", s)
			}
		})
	}
}

func TestLoadInstructionBuiltIn(t *testing.T) {
	t.Setenv("AGENT_SYSTEM_PROMPT_FILE", "")
	t.Setenv("AGENT_SKILLS_DIR", "")
	skills := fstest.MapFS{
		"b-skill/SKILL.md": {Data: []byte("---\nname: b-skill\ndescription: Second.\n---\nRule B.\n")},
		"a-skill/SKILL.md": {Data: []byte("Rule A.\n")},
		"notes.txt":        {Data: []byte("not a skill")},
		"empty/README.md":  {Data: []byte("no SKILL.md here")},
	}

	got, err := LoadInstruction("You are an agent.\n", skills)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"You are an agent.", "## Skill: a-skill", "Rule A.", "## Skill: b-skill", "Second.", "Rule B."} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "a-skill") > strings.Index(got, "b-skill") {
		t.Errorf("skills are not in name order:\n%s", got)
	}
}

func TestLoadInstructionFromFiles(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "system-prompt.md")
	if err := os.WriteFile(prompt, []byte("Prompt from OpenChoreo."), 0o600); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "mounted"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "mounted", "SKILL.md"), []byte("Mounted rule."), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_SYSTEM_PROMPT_FILE", prompt)
	t.Setenv("AGENT_SKILLS_DIR", skillsDir)

	builtIn := fstest.MapFS{"builtin/SKILL.md": {Data: []byte("Built-in rule.")}}
	got, err := LoadInstruction("Built-in prompt.", builtIn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Prompt from OpenChoreo.") || !strings.Contains(got, "Mounted rule.") {
		t.Errorf("files were not used:\n%s", got)
	}
	if strings.Contains(got, "Built-in") {
		t.Errorf("built-in prompt or skills leaked in:\n%s", got)
	}

	t.Setenv("AGENT_SKILLS_DIR", filepath.Join(dir, "missing"))
	if _, err := LoadInstruction("x", nil); err == nil {
		t.Error("want an error for a missing skills folder")
	}
}
