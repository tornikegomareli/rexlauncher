// Package skills finds what the agent in a pane can run by name, and what each of
// those does. Codex and pi say what they loaded. Claude Code has no such command, so
// its files are read in the way its documentation describes.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tornikegomareli/rexlauncher/internal/parallel"
	"gopkg.in/yaml.v3"
)

// Field is one line of a skill's frontmatter.
type Field struct {
	Label string
	Value string
}

// Skill is one thing to run by name.
type Skill struct {
	// Name is what goes in the prompt: /review, /plugin:review, $review or /skill:review.
	Name        string
	Description string
	Source      string
	Path        string
	Fields      []Field
}

// Installed is the agent in a pane and its skills. A pane with no agent has neither.
type Installed struct {
	Agent  string
	Skills []Skill
}

// Program is what runs in a pane.
type Program struct {
	Argv0 string
	Path  string // The path of the program, or of the interpreter that runs it.
	Cwd   string
}

// Load returns the skills of the agent in a pane.
func Load(p Program, claudeDir string) (Installed, error) {
	var found Installed
	var err error
	switch filepath.Base(p.Argv0) {
	case "claude":
		found.Agent, found.Skills = "Claude Code", claudeSkills(claudeDir, p.Cwd)
	case "codex":
		found.Agent = "Codex"
		found.Skills, err = codexSkills(p)
	case "pi":
		found.Agent = "pi"
		found.Skills, err = piSkills(p)
	}
	return Installed{found.Agent, inOrder(found.Skills)}, err
}

// inOrder sorts by name. When two have one name, the first wins, because the prompt
// only holds the name.
func inOrder(skills []Skill) []Skill {
	sort.SliceStable(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	var kept []Skill
	for i, s := range skills {
		if i == 0 || s.Name != skills[i-1].Name {
			kept = append(kept, s)
		}
	}
	return kept
}

// addFields reads what an agent's answer leaves out. A skill without a file only
// shows what the agent said.
func addFields(skills []Skill) {
	parallel.ForEach(len(skills), func(i int) {
		if data, err := os.ReadFile(skills[i].Path); err == nil {
			head, _ := frontmatter(string(data))
			skills[i].Fields = fields(head)
		}
	})
}

// frontmatter splits a skill's file. Text that YAML cannot read gives no fields, and
// the skill then stays under the name of its folder.
func frontmatter(text string) (map[string]any, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, text
	}
	var parsed map[string]any
	if yaml.Unmarshal([]byte(head), &parsed) != nil {
		return nil, body
	}
	return parsed, body
}

// fields is the frontmatter that is not the name or the description. A map holds
// things such as hooks that a person cannot read in one line, so only metadata stays.
func fields(head map[string]any) []Field {
	var out []Field
	add := func(label string, value any) {
		if text := oneLine(value); text != "" {
			out = append(out, Field{label, text})
		}
	}
	for label, value := range head {
		if label == "name" || label == "description" {
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			if label == "metadata" {
				for key, inner := range nested {
					add(label+"."+key, inner)
				}
			}
			continue
		}
		add(label, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func oneLine(value any) string {
	switch v := value.(type) {
	case nil, map[string]any:
		return ""
	case []any:
		var parts []string
		for _, item := range v {
			if text := oneLine(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	}
	return strings.Join(strings.Fields(fmt.Sprint(value)), " ")
}

// describe is the description of a skill. Without one, it is the first line of the text.
func describe(head map[string]any, body string) string {
	if text := oneLine(head["description"]); text != "" {
		return text
	}
	for _, line := range strings.Split(body, "\n") {
		if text := strings.TrimSpace(strings.TrimLeft(line, "# ")); text != "" {
			return text
		}
	}
	return ""
}
