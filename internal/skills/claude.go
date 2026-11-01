package skills

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tornikegomareli/rexlauncher/internal/parallel"
)

// claudeSkills lists what Claude Code's slash menu holds from files. When two have one
// name, a skill wins over a command, and personal wins over project.
func claudeSkills(dir, cwd string) []Skill {
	folders := projectFolders(cwd)
	var found []Skill
	add := func(skills []Skill) { found = append(found, skills...) }
	add(skillFolders(filepath.Join(dir, "skills"), "", "Personal"))
	for _, folder := range folders {
		add(skillFolders(filepath.Join(folder, ".claude", "skills"), "", "Project"))
	}
	add(commandFiles(filepath.Join(dir, "commands"), "", "Personal"))
	for _, folder := range folders {
		add(commandFiles(filepath.Join(folder, ".claude", "commands"), "", "Project"))
	}
	add(pluginSkills(dir, folders))
	return found
}

// projectFolders is the folder Claude Code runs in and its parents, up to the root of
// the repository. Outside a repository it is only the folder itself.
func projectFolders(cwd string) []string {
	if cwd == "" {
		return nil
	}
	var folders []string
	for at := cwd; ; at = filepath.Dir(at) {
		folders = append(folders, at)
		if _, err := os.Stat(filepath.Join(at, ".git")); err == nil {
			return folders
		}
		if at == filepath.Dir(at) {
			return []string{cwd}
		}
	}
}

// skillFolders reads each folder in root with a SKILL.md. A folder named synced is
// reserved, except in a plugin.
func skillFolders(root, plugin, source string) []Skill {
	paths, _ := filepath.Glob(filepath.Join(root, "*", "SKILL.md"))
	if plugin == "" {
		paths = slices.DeleteFunc(paths, func(path string) bool {
			return strings.EqualFold(filepath.Base(filepath.Dir(path)), "synced")
		})
	}
	return readSkills(paths, plugin, source)
}

func readSkills(paths []string, plugin, source string) []Skill {
	read := make([]*Skill, len(paths))
	parallel.ForEach(len(paths), func(i int) {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			return
		}
		head, body := frontmatter(string(data))
		if head["user-invocable"] == false {
			return // Only Claude can run it, and the slash menu hides it.
		}
		name := oneLine(head["name"])
		if name == "" {
			name = filepath.Base(filepath.Dir(paths[i]))
		}
		read[i] = &Skill{
			Name:        "/" + plugin + name,
			Description: describe(head, body),
			Source:      source,
			Path:        paths[i],
			Fields:      fields(head),
		}
	})
	return present(read)
}

func present(read []*Skill) []Skill {
	var kept []Skill
	for _, s := range read {
		if s != nil {
			kept = append(kept, *s)
		}
	}
	return kept
}

// commandFiles reads the older format: each Markdown file is a command, named by its
// path, so frontend/component.md is /frontend:component.
func commandFiles(root, plugin, source string) []Skill {
	var paths []string
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	read := make([]*Skill, len(paths))
	parallel.ForEach(len(paths), func(i int) {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			return
		}
		relative, _ := filepath.Rel(root, strings.TrimSuffix(paths[i], ".md"))
		head, body := frontmatter(string(data))
		read[i] = &Skill{
			Name:        "/" + plugin + strings.ReplaceAll(relative, string(filepath.Separator), ":"),
			Description: describe(head, body),
			Source:      source,
			Path:        paths[i],
			Fields:      fields(head),
		}
	})
	return present(read)
}

// pluginSkills reads the plugins that are on for this folder.
func pluginSkills(dir string, folders []string) []Skill {
	var installed struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			InstallPath string `json:"installPath"`
			ProjectPath string `json:"projectPath"`
		} `json:"plugins"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "plugins", "installed_plugins.json"))
	if err != nil || json.Unmarshal(data, &installed) != nil {
		return nil
	}
	enabled := enabledPlugins(dir, folders)
	var found []Skill
	for key, entries := range installed.Plugins {
		if !enabled[key] {
			continue
		}
		for _, entry := range entries {
			if entry.Scope != "user" && !slices.Contains(folders, entry.ProjectPath) {
				continue
			}
			found = append(found, oneOfPlugin(entry.InstallPath, key)...)
			break
		}
	}
	return found
}

// enabledPlugins merges the settings: the personal ones, then those of the project,
// where the nearest folder and the local file have the last word.
func enabledPlugins(dir string, folders []string) map[string]bool {
	files := []string{filepath.Join(dir, "settings.json")}
	for _, folder := range slices.Backward(folders) {
		files = append(files, filepath.Join(folder, ".claude", "settings.json"), filepath.Join(folder, ".claude", "settings.local.json"))
	}
	enabled := map[string]bool{}
	for _, file := range files {
		var settings struct {
			EnabledPlugins map[string]any `json:"enabledPlugins"`
		}
		data, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(data, &settings) != nil {
			continue
		}
		for key, value := range settings.EnabledPlugins {
			on, _ := value.(bool)
			enabled[key] = on
		}
	}
	return enabled
}

// oneOfPlugin reads a plugin's skills and commands. Its manifest can name more folders
// of skills, and each one can be a skill or hold skills.
func oneOfPlugin(root, key string) []Skill {
	var manifest struct {
		Name   string `json:"name"`
		Skills any    `json:"skills"`
	}
	if data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
		json.Unmarshal(data, &manifest)
	}
	name := manifest.Name
	if name == "" {
		name, _, _ = strings.Cut(key, "@")
	}
	source, prefix := "Plugin "+name, name+":"

	folders := []string{root, filepath.Join(root, "skills")}
	switch listed := manifest.Skills.(type) {
	case string:
		folders = append(folders, filepath.Join(root, listed))
	case []any:
		for _, item := range listed {
			if path, ok := item.(string); ok {
				folders = append(folders, filepath.Join(root, path))
			}
		}
	}
	var found []Skill
	for _, folder := range folders {
		if _, err := os.Stat(filepath.Join(folder, "SKILL.md")); err == nil {
			found = append(found, readSkills([]string{filepath.Join(folder, "SKILL.md")}, prefix, source)...)
		}
		found = append(found, skillFolders(folder, prefix, source)...)
	}
	return append(found, commandFiles(filepath.Join(root, "commands"), prefix, source)...)
}
