package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillFile(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Body\n"
}

func names(skills []Skill) string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return strings.Join(out, " ")
}

func find(t *testing.T, skills []Skill, name string) Skill {
	t.Helper()
	for _, s := range skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no %s in %s", name, names(skills))
	return Skill{}
}

func claude(t *testing.T, dir, cwd string) []Skill {
	t.Helper()
	found, err := Load(Program{Argv0: "claude", Cwd: cwd}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if found.Agent != "Claude Code" {
		t.Fatalf("agent %q", found.Agent)
	}
	return found.Skills
}

func TestAProgramThatIsNoAgentHasNoSkills(t *testing.T) {
	found, err := Load(Program{Argv0: "-zsh", Cwd: t.TempDir()}, t.TempDir())
	if err != nil || found.Agent != "" || found.Skills != nil {
		t.Fatalf("%+v, %v", found, err)
	}
}

func TestAPathToTheProgramStillNamesTheAgent(t *testing.T) {
	found, err := Load(Program{Argv0: "/Users/me/.local/bin/claude"}, t.TempDir())
	if err != nil || found.Agent != "Claude Code" {
		t.Fatalf("%+v, %v", found, err)
	}
}

func TestClaudeListsPersonalAndProjectSkillsInOrder(t *testing.T) {
	dir, repo := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "skills", "zebra", "SKILL.md"), skillFile("zebra", "Personal zebra."))
	write(t, filepath.Join(repo, ".git", "HEAD"), "")
	write(t, filepath.Join(repo, ".claude", "skills", "apple", "SKILL.md"), skillFile("apple", "At the root."))
	write(t, filepath.Join(repo, "app", ".claude", "skills", "mango", "SKILL.md"), skillFile("mango", "In the folder."))
	skills := claude(t, dir, filepath.Join(repo, "app"))
	if got := names(skills); got != "/apple /mango /zebra" {
		t.Fatalf("got %q", got)
	}
	if s := find(t, skills, "/zebra"); s.Source != "Personal" || s.Description != "Personal zebra." || !strings.HasSuffix(s.Path, "zebra/SKILL.md") {
		t.Fatalf("%+v", s)
	}
	if s := find(t, skills, "/apple"); s.Source != "Project" {
		t.Fatalf("%+v", s)
	}
}

func TestClaudeStopsAtTheRepository(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	repo := filepath.Join(outside, "repo")
	write(t, filepath.Join(repo, ".git", "HEAD"), "")
	write(t, filepath.Join(outside, ".claude", "skills", "above", "SKILL.md"), skillFile("above", "Above the repository."))
	if got := names(claude(t, dir, repo)); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAPersonalSkillWinsOverAProjectSkillAndACommand(t *testing.T) {
	dir, repo := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "skills", "same", "SKILL.md"), skillFile("same", "Personal."))
	write(t, filepath.Join(repo, ".claude", "skills", "same", "SKILL.md"), skillFile("same", "Project."))
	write(t, filepath.Join(repo, ".claude", "commands", "same.md"), "A command.\n")
	skills := claude(t, dir, repo)
	if got := names(skills); got != "/same" {
		t.Fatalf("got %q", got)
	}
	if s := find(t, skills, "/same"); s.Description != "Personal." {
		t.Fatalf("%+v", s)
	}
}

func TestClaudeHidesWhatOnlyClaudeCanRun(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "hidden", "SKILL.md"), "---\nname: hidden\nuser-invocable: false\n---\n")
	write(t, filepath.Join(dir, "skills", "manual", "SKILL.md"), "---\nname: manual\ndisable-model-invocation: true\n---\n")
	if got := names(claude(t, dir, "")); got != "/manual" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeSkipsTheReservedFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "synced", "SKILL.md"), skillFile("synced", "Reserved."))
	if got := names(claude(t, dir, "")); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeFollowsALinkToASkill(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	write(t, filepath.Join(elsewhere, "linked", "SKILL.md"), skillFile("linked", "From a link."))
	os.MkdirAll(filepath.Join(dir, "skills"), 0o755)
	if err := os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(dir, "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	if got := names(claude(t, dir, "")); got != "/linked" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeListsCommandsByTheirPath(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "commands", "deploy.md"), "---\ndescription: Ship it.\nargument-hint: [env]\n---\nDo it.\n")
	write(t, filepath.Join(dir, "commands", "frontend", "component.md"), "Make a component.\n")
	skills := claude(t, dir, "")
	if got := names(skills); got != "/deploy /frontend:component" {
		t.Fatalf("got %q", got)
	}
	if s := find(t, skills, "/frontend:component"); s.Description != "Make a component." {
		t.Fatalf("%+v", s)
	}
}

// plugins writes what Claude Code keeps about plugins: where they are, and which are on.
func plugins(t *testing.T, dir, installed string, enabled string) {
	t.Helper()
	write(t, filepath.Join(dir, "plugins", "installed_plugins.json"), `{"version":2,"plugins":{`+installed+`}}`)
	write(t, filepath.Join(dir, "settings.json"), `{"enabledPlugins":{`+enabled+`}}`)
}

func entry(key, scope, path, project string) string {
	return `"` + key + `":[{"scope":"` + scope + `","installPath":"` + path + `","projectPath":"` + project + `"}]`
}

func TestClaudeNamesAPluginSkillAfterItsPlugin(t *testing.T) {
	dir, plugin := t.TempDir(), t.TempDir()
	write(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"tools"}`)
	write(t, filepath.Join(plugin, "skills", "review", "SKILL.md"), skillFile("review", "Review it."))
	write(t, filepath.Join(plugin, "commands", "ship.md"), "Ship it.\n")
	plugins(t, dir, entry("tools@market", "user", plugin, ""), `"tools@market":true`)
	skills := claude(t, dir, "")
	if got := names(skills); got != "/tools:review /tools:ship" {
		t.Fatalf("got %q", got)
	}
	if s := find(t, skills, "/tools:review"); s.Source != "Plugin tools" {
		t.Fatalf("%+v", s)
	}
}

func TestClaudeReadsThePluginManifestForSkillFolders(t *testing.T) {
	dir, plugin := t.TempDir(), t.TempDir()
	write(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"tools","skills":["./skills/group/deep"]}`)
	write(t, filepath.Join(plugin, "skills", "group", "deep", "SKILL.md"), skillFile("deep", "Nested."))
	plugins(t, dir, entry("tools@market", "user", plugin, ""), `"tools@market":true`)
	if got := names(claude(t, dir, "")); got != "/tools:deep" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeListsOnlyEnabledPluginsAndThoseOfThisProject(t *testing.T) {
	dir, off, local, repo, other := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(off, "skills", "off", "SKILL.md"), skillFile("off", "Off."))
	write(t, filepath.Join(local, "skills", "here", "SKILL.md"), skillFile("here", "Here."))
	plugins(t, dir, entry("off@m", "user", off, "")+","+entry("local@m", "local", local, repo), `"off@m":false`)
	write(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"enabledPlugins":{"local@m":true}}`)
	if got := names(claude(t, dir, other)); got != "" {
		t.Fatalf("other project: %q", got)
	}
	if got := names(claude(t, dir, repo)); got != "/local:here" {
		t.Fatalf("this project: %q", got)
	}
}

func TestBrokenFrontmatterKeepsTheSkillUnderItsFolderName(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "broken", "SKILL.md"), "---\nname: x\ndescription: Use when: it breaks\n---\n\n# Broken body\n")
	skills := claude(t, dir, "")
	if s := find(t, skills, "/broken"); s.Description != "Broken body" {
		t.Fatalf("%+v", s)
	}
}

func TestAMissingDescriptionIsTheFirstLineOfTheText(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "plain", "SKILL.md"), "\n\nFirst line.\nSecond.\n")
	if s := find(t, claude(t, dir, ""), "/plain"); s.Description != "First line." {
		t.Fatalf("%+v", s)
	}
}

func TestAFoldedDescriptionIsOneParagraph(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "long", "SKILL.md"), "---\ndescription: |\n  One line.\n  Two lines.\n---\n")
	if s := find(t, claude(t, dir, ""), "/long"); s.Description != "One line. Two lines." {
		t.Fatalf("%+v", s)
	}
}

func TestFieldsAreTheRestOfTheFrontmatter(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "skills", "full", "SKILL.md"), `---
name: full
description: All of it.
version: 1.2
allowed-tools: [Read, Grep]
disable-model-invocation: true
metadata:
  author: someone
hooks:
  PreToolUse:
    - matcher: Bash
---
`)
	var got []string
	for _, f := range find(t, claude(t, dir, ""), "/full").Fields {
		got = append(got, f.Label+"="+f.Value)
	}
	want := "allowed-tools=Read, Grep|disable-model-invocation=true|metadata.author=someone|version=1.2"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %q", strings.Join(got, "|"))
	}
}

// agentProgram makes a program that prints an answer, in a folder of its own.
func agentProgram(t *testing.T, name, answer string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$*\" > \"" + dir + "/args\"\npwd > \"" + dir + "/cwd\"\nhead -n 1 > /dev/null\ncat <<'END'\n" + answer + "\nEND\n"
	write(t, filepath.Join(dir, name), script)
	if err := os.Chmod(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestCodexIsAskedAndAMentionUsesItsSign(t *testing.T) {
	file := filepath.Join(t.TempDir(), "SKILL.md")
	write(t, file, "---\nname: review\ndescription: Review it.\nlicense: MIT\n---\n")
	answer := `{"id":0,"result":{}}
{"id":1,"result":{"data":[{"cwd":"/x","errors":[],"skills":[
`
	answer = strings.ReplaceAll(answer, "[\n", "[")
	answer += `{"name":"review","description":"Review it.","path":"` + file + `","scope":"user","enabled":true,"pluginId":null},` +
		`{"name":"off","description":"Off.","path":"/none","scope":"user","enabled":false,"pluginId":null},` +
		`{"name":"sys","description":"Built.","path":"/none","scope":"system","enabled":true,"pluginId":null},` +
		`{"name":"repo","description":"Here.","path":"/none","scope":"repo","enabled":true,"pluginId":null},` +
		`{"name":"plug","description":"Plugged.","path":"/none","scope":"user","enabled":true,"pluginId":"atlas@atlas"}]}]}}`
	program := agentProgram(t, "codex", answer)
	cwd := t.TempDir()
	found, err := Load(Program{Argv0: "codex", Path: program, Cwd: cwd}, "")
	if err != nil {
		t.Fatal(err)
	}
	if found.Agent != "Codex" || names(found.Skills) != "$plug $repo $review $sys" {
		t.Fatalf("%q %q", found.Agent, names(found.Skills))
	}
	for name, source := range map[string]string{"$review": "Personal", "$repo": "Project", "$sys": "Built in", "$plug": "Plugin atlas"} {
		if s := find(t, found.Skills, name); s.Source != source {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if s := find(t, found.Skills, "$review"); len(s.Fields) != 1 || s.Fields[0].Value != "MIT" {
		t.Fatalf("no fields from the file: %+v", s)
	}
}

func TestPiIsAskedWithoutItsExtensionsAndOnlySkillsCount(t *testing.T) {
	file := filepath.Join(t.TempDir(), "SKILL.md")
	write(t, file, skillFile("web", "Search."))
	program := agentProgram(t, "pi", `{"type":"response","command":"get_commands","success":true,"data":{"commands":[`+
		`{"name":"fix","description":"A template.","source":"prompt"},`+
		`{"name":"skill:web","description":"Search.","source":"skill","sourceInfo":{"path":"`+file+`","scope":"project"}}]}}`)
	found, err := Load(Program{Argv0: "pi", Path: program, Cwd: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if found.Agent != "pi" || names(found.Skills) != "/skill:web" {
		t.Fatalf("%q %q", found.Agent, names(found.Skills))
	}
	if s := find(t, found.Skills, "/skill:web"); s.Source != "Project" || s.Path != file {
		t.Fatalf("%+v", s)
	}
	args, _ := os.ReadFile(filepath.Join(filepath.Dir(program), "args"))
	if !strings.Contains(string(args), "-ne") {
		t.Fatalf("args %q", args)
	}
}

func TestAnAgentThatDoesNotAnswerIsAnError(t *testing.T) {
	program := agentProgram(t, "codex", `not json`)
	found, err := Load(Program{Argv0: "codex", Path: program, Cwd: t.TempDir()}, "")
	if err == nil {
		t.Fatalf("no error: %+v", found)
	}
	if found.Agent != "Codex" {
		t.Fatalf("agent %q", found.Agent)
	}
}

func TestTheProgramIsFoundBesideItsInterpreter(t *testing.T) {
	// pi runs in node, so Rex names node, and pi is in the same folder.
	program := agentProgram(t, "pi", `{"type":"response","command":"get_commands","success":true,"data":{"commands":[]}}`)
	if _, err := Load(Program{Argv0: "pi", Path: filepath.Join(filepath.Dir(program), "node"), Cwd: t.TempDir()}, ""); err != nil {
		t.Fatal(err)
	}
}
