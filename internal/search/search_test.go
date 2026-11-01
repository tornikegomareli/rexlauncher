package search

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tornikegomareli/rexlauncher/internal/conversations"
	"github.com/tornikegomareli/rexlauncher/internal/rex"
	"github.com/tornikegomareli/rexlauncher/internal/skills"
	"github.com/tornikegomareli/rexlauncher/internal/textutil"
)

func said(role, text string) conversations.Message {
	return conversations.Message{Role: role, At: "t", Text: text}
}

// talking is a pane with a conversation.
func talking(block, name string, messages ...conversations.Message) rex.Pane {
	return rex.Pane{Block: block, Name: name, Conversation: &conversations.Conversation{}, Text: MessageEntries(messages)}
}

// showing is a pane with text on its screen.
func showing(block, name string, text string) rex.Pane {
	return rex.Pane{Block: block, Name: name, Text: ScreenEntries(text)}
}

func kinds(results []Result) []string {
	names := map[Reason]string{reasonRecent: "recent", reasonName: "pane", reasonSaid: "said", reasonScreen: "screen"}
	var out []string
	for _, r := range results {
		out = append(out, names[r.Reason]+":"+r.Pane.Block)
	}
	return out
}

func TestEmptyQueryListsEveryPaneOnly(t *testing.T) {
	panes := []rex.Pane{talking("a", "one", said("user", "hello")), {Block: "b", Name: "two"}}
	got := kinds(Search(panes, "  "))
	if strings.Join(got, " ") != "pane:a pane:b" {
		t.Fatalf("got %v", got)
	}
}

func TestPanesComeBeforeWordsAndWordsBeforeScreens(t *testing.T) {
	panes := []rex.Pane{
		showing("screen", "shell", "flock failed"),
		talking("talk", "shell", said("assistant", "the flock call")),
		{Block: "named", Name: "flock work"},
	}
	results := Search(panes, "flock")
	if got := strings.Join(kinds(results), " "); got != "pane:named said:talk screen:screen" {
		t.Fatalf("got %v", got)
	}
	var headings []string
	for _, r := range results {
		headings = append(headings, r.Heading())
	}
	if got := strings.Join(headings, " "); got != "Panes Conversations Screens" {
		t.Fatalf("got %v", got)
	}
}

func TestAPaneHasOneRowHoweverManyMessagesMatch(t *testing.T) {
	var lines []conversations.Message
	for i := range 40 {
		lines = append(lines, said("user", fmt.Sprintf("word %d", i)))
	}
	results := Search([]rex.Pane{talking("a", "x", lines...)}, "word")
	if len(results) != 1 || results[0].Matches() != 40 {
		t.Fatalf("got %+v", results)
	}
}

func TestAMatchAtTheStartOfAWordBeatsAMatchInsideOne(t *testing.T) {
	panes := []rex.Pane{
		talking("inside", "x", said("user", "I read Japanese")),
		showing("start", "y", "split panes here"),
	}
	results := Search(panes, "panes")
	if got := strings.Join(kinds(results), " "); got != "screen:start said:inside" {
		t.Fatalf("got %v", got)
	}
	if got := results[1].Heading(); got != "Partial matches" {
		t.Fatalf("got %v", got)
	}
	// The word a person is still typing counts too.
	if got := strings.Join(kinds(Search(panes, "pane")), " "); !strings.HasPrefix(got, "screen:start") {
		t.Fatalf("got %v", got)
	}
}

func TestTheBestMatchComesFirst(t *testing.T) {
	p := talking("a", "x", said("user", "older panes"), said("user", "newer panes"), said("user", "newest Japanese"))
	found := Search([]rex.Pane{p}, "panes")[0].Found
	if len(found) != 3 || found[0].Text != "newer panes" || found[1].Text != "older panes" || found[2].Text != "newest Japanese" {
		t.Fatalf("got %+v", found)
	}
}

func TestTheNewestConversationComesFirst(t *testing.T) {
	line := func(at string) conversations.Message {
		return conversations.Message{Role: "user", At: at, Text: "word"}
	}
	panes := []rex.Pane{
		talking("old", "x", line("2026-01-01T00:00:00Z")),
		talking("new", "y", line("2026-06-01T00:00:00Z")),
	}
	if got := strings.Join(kinds(Search(panes, "word")), " "); got != "said:new said:old" {
		t.Fatalf("got %v", got)
	}
}

func TestSearchIgnoresCaseAndNeedsEveryWord(t *testing.T) {
	panes := []rex.Pane{talking("a", "x", said("user", "Fix the FLOCK bug"), said("user", "flock only"))}
	results := Search(panes, "bug flock")
	if len(results) != 1 || results[0].Matches() != 1 {
		t.Fatalf("got %+v", results)
	}
}

func TestSearchFindsAnyAlphabet(t *testing.T) {
	panes := []rex.Pane{talking("a", "x", said("user", "გამარჯობა Мир"))}
	for _, query := range []string{"გამარჯობა", "мир"} {
		if got := Search(panes, query); len(got) != 1 {
			t.Errorf("%s: got %d results", query, len(got))
		}
	}
}

func TestIdenticalMessagesCountOnce(t *testing.T) {
	p := talking("a", "x", said("user", "a word"), said("assistant", "other"), said("user", "a word"))
	if got := Search([]rex.Pane{p}, "word")[0].Matches(); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestMarkdownIsNotKeptOrSearched(t *testing.T) {
	p := talking("a", "x", said("assistant", "## Result **the** `word` [Image #9] here"))
	if got := p.Text[0].Text; got != "Result the word here" {
		t.Fatalf("got %q", got)
	}
	if got := Search([]rex.Pane{p}, "image"); len(got) != 0 {
		t.Errorf("found an image tag: %+v", got)
	}
}

func TestControlCharactersAreNotKept(t *testing.T) {
	said := talking("a", "x", said("user", "a word \x1b[2J here"))
	screen := showing("b", "y", "a word \x1b[2J here")
	for _, p := range []rex.Pane{said, screen} {
		if strings.Contains(p.Text[0].Text, "\x1b") {
			t.Fatalf("got %q", p.Text[0].Text)
		}
	}
}

func TestAround(t *testing.T) {
	long := strings.Repeat("a ", 150) + "needle" + strings.Repeat(" b", 150)
	got := Around(long, "needle", 20, 80)
	if !strings.Contains(got, "needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
	if strings.Index(got, "needle") > 25 {
		t.Fatalf("the word is far from the start: %q", got)
	}
	if short := "a needle"; Around(short, "needle", 20, 80) != short {
		t.Fatal("changed short text")
	}
	if !strings.Contains(Around(strings.Repeat("a", 300)+"needle", "needle", 20, 80), "needle") {
		t.Fatal("lost a word at the end")
	}
	if !strings.Contains(Around(strings.Repeat("ა", 300)+"needle"+strings.Repeat("ბ", 300), "needle", 20, 80), "needle") {
		t.Fatal("lost a word after multi-byte text")
	}
}

func TestAroundStartsAtAWholeWord(t *testing.T) {
	got := Around("first reduced-motion benchmark and then the needle is here", "needle", 20, 80)
	if !strings.HasPrefix(got, "…and then the needle") && !strings.HasPrefix(got, "…then the needle") {
		t.Fatalf("got %q", got)
	}
}

func TestAroundPrefersTheStartOfAWord(t *testing.T) {
	got := Around("Japanese "+strings.Repeat("x ", 100)+"panes", "panes", 5, 20)
	if !strings.HasSuffix(got, "panes") {
		t.Fatalf("got %q", got)
	}
}

func TestMarks(t *testing.T) {
	got := Marks("a Word", []string{"word"})
	want := []bool{false, false, true, true, true, true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v", got)
	}
	if got := Marks("ა word", []string{"word"}); len(got) != 6 || !got[2] {
		t.Fatalf("multi-byte text: %v", got)
	}
}

func TestANameMatchesWithLettersLeftOut(t *testing.T) {
	for _, c := range []struct{ query, name string }{
		{"tlbr", "tilebar"},
		{"jl", "jolt-landing"},
		{"splt", "Split pane"},
	} {
		results := Search([]rex.Pane{{Block: "a", Name: c.name}}, c.query)
		if len(results) != 1 || results[0].Reason != reasonName || results[0].Heading() != "Panes" {
			t.Errorf("%s in %s: got %+v", c.query, c.name, results)
		}
	}
}

func TestLettersFarApartDoNotMatchAName(t *testing.T) {
	for _, c := range []struct{ query, name string }{
		{"panes", "Panel of notes"},
		{"tlbr", "the long road home to bar"},
		{"r", "tilebar"},
		{"ilebar", "tilebar x"}, // Found as text in a word, and it is not at the start of one.
	} {
		for _, r := range Search([]rex.Pane{{Block: "a", Name: c.name}}, c.query) {
			if r.Letters != nil {
				t.Errorf("%s in %s: got %+v", c.query, c.name, r)
			}
		}
	}
}

func TestAWholeNameComesBeforeLettersLeftOut(t *testing.T) {
	panes := []rex.Pane{{Block: "letters", Name: "tilebar"}, {Block: "whole", Name: "tlbr tools"}}
	if got := strings.Join(kinds(Search(panes, "tlbr")), " "); got != "pane:whole pane:letters" {
		t.Fatalf("got %v", got)
	}
}

func TestWordsInThePaneBeatLettersOfItsName(t *testing.T) {
	panes := []rex.Pane{{Block: "letters", Name: "tilebar"}, talking("talk", "x", said("user", "the tlbr file"))}
	results := Search(panes, "tlbr")
	if got := strings.Join(kinds(results), " "); got != "pane:letters said:talk" {
		t.Fatalf("got %v", got)
	}
	both := talking("both", "tilebar", said("user", "the tlbr file"))
	if got := Search([]rex.Pane{both}, "tlbr")[0]; got.Reason != reasonSaid || got.Letters != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestAnEmptyQueryShowsRecentConversationsFirst(t *testing.T) {
	now := time.Now()
	old, recent := rex.Pane{Block: "old", Name: "o", Conversation: &conversations.Conversation{Active: now.Add(-time.Hour)}}, rex.Pane{Block: "new", Name: "n", Conversation: &conversations.Conversation{Active: now}}
	here := rex.Pane{Block: "here", Name: "h", Conversation: &conversations.Conversation{Active: now.Add(time.Minute)}, Here: true}
	shell := rex.Pane{Block: "shell", Name: "s"}
	results := Search([]rex.Pane{shell, old, here, recent}, "")
	var got []string
	for _, r := range results {
		got = append(got, r.Heading()+":"+r.Pane.Block)
	}
	want := "Recent conversations:new Recent conversations:old Panes:shell Panes:here"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

func TestTheRecentSectionIsLimited(t *testing.T) {
	var panes []rex.Pane
	for i := range maxRecent + 3 {
		panes = append(panes, rex.Pane{Block: fmt.Sprint(i), Name: "x", Conversation: &conversations.Conversation{Active: time.Now().Add(time.Duration(i) * time.Minute)}})
	}
	results := Search(panes, "")
	if results[0].Pane.Block != fmt.Sprint(maxRecent+2) || results[maxRecent-1].Heading() != "Recent conversations" || results[maxRecent].Heading() != "Panes" {
		t.Fatalf("got %+v", results[maxRecent])
	}
}

func actionNames(results []Result) []string {
	var names []string
	for _, r := range results {
		if r.Action != nil {
			names = append(names, r.Action.Name)
		}
	}
	return names
}

var someActions = []rex.Action{
	{Name: "pane.split.down", Title: "Split Pane Down", Keywords: "pane split terminal divide", Category: "Pane"},
	{Name: "pane.split.right", Title: "Split Pane Right", Keywords: "pane split terminal divide", Category: "Pane"},
	{Name: "client.theme.change", Title: "Change Theme…", Keywords: "theme colour color scheme", Category: "View"},
	{Name: "client.sidebar.toggle", Title: "Toggle Sidebar", Keywords: "terminal", Category: "View"},
}

func TestSearchActionsPutsTitlesBeforeKeywords(t *testing.T) {
	got := actionNames(SearchActions(someActions, "terminal"))
	if strings.Join(got, " ") != "pane.split.down pane.split.right client.sidebar.toggle" {
		t.Fatalf("keywords: %q", got)
	}
	got = actionNames(SearchActions([]rex.Action{
		{Name: "by-keyword", Title: "Reset", Keywords: "theme"},
		{Name: "by-title", Title: "Change Theme…"},
	}, "theme"))
	if strings.Join(got, " ") != "by-title by-keyword" {
		t.Fatalf("order: %q", got)
	}
}

func skillNames(results []Result) string {
	var names []string
	for _, r := range results {
		names = append(names, r.Skill.Name)
	}
	return strings.Join(names, " ")
}

func TestSearchSkillsPutsNamesBeforeDescriptions(t *testing.T) {
	installed := []skills.Skill{
		{Name: "/code-review", Description: "Look at a diff."},
		{Name: "/notes", Description: "Write a review of the week."},
		{Name: "/plugin:review-pr", Description: "Look at a pull request."},
		{Name: "/tests", Description: "Run the tests."},
	}
	if got := skillNames(SearchSkills(installed, "review")); got != "/plugin:review-pr /code-review /notes" {
		t.Fatalf("got %q", got)
	}
	if got := skillNames(SearchSkills(installed, "/code")); got != "/code-review" {
		t.Fatalf("with its sign: %q", got)
	}
	if got := skillNames(SearchSkills(installed, "")); got != "/code-review /notes /plugin:review-pr /tests" {
		t.Fatalf("empty query: %q", got)
	}
}

func TestAllPutsSkillsAfterPanesAndBeforeActionsAndConversations(t *testing.T) {
	said := rex.Pane{Block: "said", Conversation: &conversations.Conversation{}, Text: []textutil.Entry{{Text: "review it", Lower: "review it"}}}
	named := rex.Pane{Block: "named", SessionLabel: "review", Name: "shell"}
	rows := All([]rex.Pane{said, named}, []skills.Skill{{Name: "/review"}}, []rex.Action{{Name: "a", Title: "Review Action"}}, "review")
	var order []string
	for _, r := range rows {
		order = append(order, r.Heading())
	}
	if got := strings.Join(order, " "); got != "Panes Skills Actions Conversations" {
		t.Fatalf("got %q", got)
	}
}

func waiting(block, state string, at time.Time) rex.Pane {
	return rex.Pane{Block: block, Name: block, Status: rex.Status{State: state, At: at}}
}

func TestAnEmptyQueryPutsWhatNeedsThePersonFirst(t *testing.T) {
	now := time.Now()
	rows := Search([]rex.Pane{
		{Block: "plain", Name: "plain"},
		{Block: "recent", Name: "recent", Conversation: &conversations.Conversation{Active: now}},
		waiting("working", rex.StateWorking, now),
		waiting("done", rex.StateDone, now),
		waiting("error", rex.StateError, now.Add(-time.Minute)),
		waiting("blocked", rex.StateBlocked, now),
		waiting("idle", rex.StateIdle, now),
	}, "")
	var got []string
	for _, r := range rows {
		got = append(got, r.Heading()+":"+r.Pane.Block)
	}
	want := "Needs you:blocked|Needs you:error|Finished:done|Working:working|Recent conversations:recent|Panes:plain|Panes:idle"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %q", strings.Join(got, "|"))
	}
}

func TestABlockedPaneInThePickersWindowStillNeedsThePerson(t *testing.T) {
	here := waiting("here", rex.StateBlocked, time.Now())
	here.Here = true
	if rows := Search([]rex.Pane{here}, ""); rows[0].Heading() != "Needs you" {
		t.Fatalf("got %q", rows[0].Heading())
	}
}

func TestThePaneThePickerOpenedFromIsLeftOutOfTheInbox(t *testing.T) {
	from := waiting("from", rex.StateBlocked, time.Now())
	from.From = true
	if rows := Search([]rex.Pane{from}, ""); rows[0].Heading() != "Panes" {
		t.Fatalf("got %q", rows[0].Heading())
	}
}

func TestFinishedPanesAreLimited(t *testing.T) {
	var panes []rex.Pane
	for i := range maxRecent + 3 {
		panes = append(panes, waiting(fmt.Sprint(i), rex.StateDone, time.Now()))
	}
	finished := 0
	for _, r := range Search(panes, "") {
		if r.Heading() == "Finished" {
			finished++
		}
	}
	if finished != maxRecent {
		t.Fatalf("%d finished", finished)
	}
}

func TestAStatusMessageIsSearched(t *testing.T) {
	p := rex.Pane{Block: "b", Name: "agent", Status: rex.Status{State: rex.StateBlocked, Message: "Allow terraform apply?"}}
	if rows := Search([]rex.Pane{p}, "terraform"); len(rows) != 1 {
		t.Fatalf("got %+v", rows)
	}
}
