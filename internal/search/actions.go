package search

import (
	"sort"
	"strings"

	"github.com/tornikegomareli/rexlauncher/internal/layouts"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// MaxActionsInAll keeps actions from pushing conversations out of view when a word,
// such as "session", is in many titles. The Actions tab lists them all.
const MaxActionsInAll = 4

// MaxSkillsInAll does the same for skills, and the Skills tab lists them all.
const MaxSkillsInAll = 4

// MaxLayoutsInAll does the same for layouts.
const MaxLayoutsInAll = 3

// searchActions returns the actions with the words in their titles, then those with
// them in their keywords. An empty query returns them all.
func SearchActions(actions []rex.Action, query string) []Result {
	words := strings.Fields(textutil.Lower(query))
	var titled, other []Result
	for i := range actions {
		a := &actions[i]
		r := Result{Action: a, Reason: ReasonAction}
		switch title := textutil.Lower(a.Title); {
		case containsAll(title, words):
			titled = append(titled, r)
		case containsAll(title+" "+textutil.Lower(a.Keywords), words):
			other = append(other, r)
		}
	}
	return append(titled, other...)
}

// SearchSkills returns the skills with the words in their names, then those with them
// in their descriptions. A skill whose name starts with the query comes first, because
// its name is what a person remembers. An empty query returns them all.
func SearchSkills(installed []skills.Skill, query string) []Result {
	words := strings.Fields(textutil.Lower(query))
	var starting, named, described []Result
	for i := range installed {
		s := &installed[i]
		r := Result{Skill: s, Reason: ReasonSkill}
		name := textutil.Lower(s.Name)
		switch {
		case containsAll(name, words) && len(words) > 0 && strings.HasPrefix(bareName(name), words[0]):
			starting = append(starting, r)
		case containsAll(name, words):
			named = append(named, r)
		case containsAll(name+" "+textutil.Lower(s.Description), words):
			described = append(described, r)
		}
	}
	return append(append(starting, named...), described...)
}

// bareName is a skill's name without its sign and its plugin.
func bareName(name string) string {
	return name[strings.LastIndexAny(name, "/$:")+1:]
}

// SearchLayouts returns the row that saves a layout, if the words fit it, and then the
// layouts with the words in their names or their panes.
func SearchLayouts(saved []layouts.Layout, query string) []Result {
	words := strings.Fields(textutil.Lower(query))
	var rows []Result
	if containsAll("save this layout", words) {
		rows = append(rows, Result{SaveLayout: true, Reason: ReasonLayout})
	}
	for i := range saved {
		l := &saved[i]
		text := l.Name
		for _, p := range l.Panes() {
			text += " " + p.Title + " " + ShortHome(p.Cwd)
		}
		if containsAll(textutil.Lower(text), words) {
			rows = append(rows, Result{Layout: l, Reason: ReasonLayout})
		}
	}
	return rows
}

// All returns the rows for a query, panes, skills, layouts and actions together, in the
// order of their sections.
func All(panes []rex.Pane, installed []skills.Skill, saved []layouts.Layout, actions []rex.Action, query string) []Result {
	rows := append(Search(panes, query), SearchSkills(installed, query)...)
	rows = append(rows, SearchLayouts(saved, query)...)
	rows = append(rows, SearchActions(actions, query)...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].before(rows[j]) })
	return rows
}
