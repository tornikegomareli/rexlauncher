package search

import (
	"sort"
	"strings"

	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

// MaxActionsInAll keeps actions from pushing conversations out of view when a word,
// such as "session", is in many titles. The Actions tab lists them all.
const MaxActionsInAll = 4

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

// All returns the rows for a query, panes and actions together, in the order of their
// sections.
func All(panes []rex.Pane, actions []rex.Action, query string) []Result {
	rows := append(Search(panes, query), SearchActions(actions, query)...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].before(rows[j]) })
	return rows
}
