package kit

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A skill is a folder with a SKILL.md, as in the Agent Skills format. kagent
// copies each skill of the Agent to /skills/<name>.
type skill struct {
	name, description, body string
}

// withSkills appends the skills to the system prompt.
func withSkills(prompt string, skills []skill) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	if len(skills) > 0 {
		b.WriteString("\n# Skills\n\nThe skills below are part of your instructions. Follow them.\n")
		for _, s := range skills {
			fmt.Fprintf(&b, "\n## Skill: %s\n\n", s.name)
			if s.description != "" {
				fmt.Fprintf(&b, "%s\n\n", s.description)
			}
			b.WriteString(s.body)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// readSkills reads every folder of fsys that holds a SKILL.md, in name order.
func readSkills(fsys fs.FS) ([]skill, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var skills []skill
	for _, e := range entries {
		// fs.Stat follows symlinks, which a ReadDir entry does not.
		info, err := fs.Stat(fsys, e.Name())
		if err != nil || !info.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(e.Name(), "SKILL.md"))
		if errors.Is(err, fs.ErrNotExist) {
			slog.Warn("skipping a skill folder without SKILL.md", "folder", e.Name())
			continue
		}
		if err != nil {
			return nil, err
		}
		s, err := parseSkill(e.Name(), string(raw))
		if err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// parseSkill splits a SKILL.md into its YAML frontmatter (name and description)
// and its body. A file without frontmatter is all body, named after its folder.
func parseSkill(folder, text string) (skill, error) {
	s := skill{name: folder}
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")

	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		front, body, found := strings.Cut(rest, "\n---")
		if !found {
			return s, fmt.Errorf("%s/SKILL.md: the frontmatter has no closing ---", folder)
		}
		var meta struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
			return s, fmt.Errorf("%s/SKILL.md: invalid frontmatter: %w", folder, err)
		}
		if meta.Name != "" {
			s.name = meta.Name
		}
		s.description = strings.TrimSpace(meta.Description)
		// Drop the rest of the closing --- line.
		if _, after, ok := strings.Cut(body, "\n"); ok {
			text = after
		} else {
			text = ""
		}
	}

	s.body = strings.TrimSpace(text)
	if s.body == "" {
		return s, fmt.Errorf("%s/SKILL.md: the skill has no instructions", folder)
	}
	return s, nil
}
