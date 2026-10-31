package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func said(role, text string) string {
	return strings.TrimSuffix(message{Role: role, At: "t", Text: text}.line(), "\n")
}

// talking is a pane with a conversation.
func talking(t *testing.T, block, name string, lines ...string) pane {
	t.Helper()
	return pane{Block: block, Name: name, Conversation: true, Text: messageEntries(writeLines(t, lines...))}
}

// showing is a pane with text on its screen.
func showing(block, name string, text string) pane {
	return pane{Block: block, Name: name, Text: screenEntries(text)}
}

func kinds(results []result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Kind+":"+r.Pane.Block)
	}
	return out
}

func TestEmptyQueryListsEveryPaneOnly(t *testing.T) {
	panes := []pane{talking(t, "a", "one", said("user", "hello")), {Block: "b", Name: "two"}}
	got := kinds(search(panes, "  "))
	if strings.Join(got, " ") != "pane:a pane:b" {
		t.Fatalf("got %v", got)
	}
}

func TestPanesComeBeforeWordsAndWordsBeforeScreens(t *testing.T) {
	panes := []pane{
		showing("screen", "shell", "flock failed"),
		talking(t, "talk", "shell", said("assistant", "the flock call")),
		{Block: "named", Name: "flock work"},
	}
	results := search(panes, "flock")
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
	var lines []string
	for i := range 40 {
		lines = append(lines, said("user", fmt.Sprintf("word %d", i)))
	}
	results := search([]pane{talking(t, "a", "x", lines...)}, "word")
	if len(results) != 1 || results[0].Matches() != 40 {
		t.Fatalf("got %+v", results)
	}
}

func TestAMatchAtTheStartOfAWordBeatsAMatchInsideOne(t *testing.T) {
	panes := []pane{
		talking(t, "inside", "x", said("user", "I read Japanese")),
		showing("start", "y", "split panes here"),
	}
	results := search(panes, "panes")
	if got := strings.Join(kinds(results), " "); got != "screen:start said:inside" {
		t.Fatalf("got %v", got)
	}
	if got := results[1].Heading(); got != "Partial matches" {
		t.Fatalf("got %v", got)
	}
	// The word a person is still typing counts too.
	if got := strings.Join(kinds(search(panes, "pane")), " "); !strings.HasPrefix(got, "screen:start") {
		t.Fatalf("got %v", got)
	}
}

func TestTheBestMatchComesFirst(t *testing.T) {
	p := talking(t, "a", "x", said("user", "older panes"), said("user", "newer panes"), said("user", "newest Japanese"))
	found := search([]pane{p}, "panes")[0].Found
	if len(found) != 3 || found[0].Text != "newer panes" || found[1].Text != "older panes" || found[2].Text != "newest Japanese" {
		t.Fatalf("got %+v", found)
	}
}

func TestTheNewestConversationComesFirst(t *testing.T) {
	line := func(at string) string {
		return strings.TrimSuffix(message{Role: "user", At: at, Text: "word"}.line(), "\n")
	}
	panes := []pane{
		talking(t, "old", "x", line("2026-01-01T00:00:00Z")),
		talking(t, "new", "y", line("2026-06-01T00:00:00Z")),
	}
	if got := strings.Join(kinds(search(panes, "word")), " "); got != "said:new said:old" {
		t.Fatalf("got %v", got)
	}
}

func TestSearchIgnoresCaseAndNeedsEveryWord(t *testing.T) {
	panes := []pane{talking(t, "a", "x", said("user", "Fix the FLOCK bug"), said("user", "flock only"))}
	results := search(panes, "bug flock")
	if len(results) != 1 || results[0].Matches() != 1 {
		t.Fatalf("got %+v", results)
	}
}

func TestSearchFindsAnyAlphabet(t *testing.T) {
	panes := []pane{talking(t, "a", "x", said("user", "გამარჯობა Мир"))}
	for _, query := range []string{"გამარჯობა", "мир"} {
		if got := search(panes, query); len(got) != 1 {
			t.Errorf("%s: got %d results", query, len(got))
		}
	}
}

func TestIdenticalMessagesCountOnce(t *testing.T) {
	p := talking(t, "a", "x", said("user", "a word"), said("assistant", "other"), said("user", "a word"))
	if got := search([]pane{p}, "word")[0].Matches(); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestMarkdownIsNotKeptOrSearched(t *testing.T) {
	p := talking(t, "a", "x", said("assistant", "## Result **the** `word` [Image #9] here"))
	if got := p.Text[0].Text; got != "Result the word here" {
		t.Fatalf("got %q", got)
	}
	if got := search([]pane{p}, "image"); len(got) != 0 {
		t.Errorf("found an image tag: %+v", got)
	}
}

func TestControlCharactersAreNotKept(t *testing.T) {
	said := talking(t, "a", "x", said("user", "a word \x1b[2J here"))
	screen := showing("b", "y", "a word \x1b[2J here")
	for _, p := range []pane{said, screen} {
		if strings.Contains(p.Text[0].Text, "\x1b") {
			t.Fatalf("got %q", p.Text[0].Text)
		}
	}
}

func TestAround(t *testing.T) {
	long := strings.Repeat("a ", 150) + "needle" + strings.Repeat(" b", 150)
	got := around(long, "needle", 20, 80)
	if !strings.Contains(got, "needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
	if strings.Index(got, "needle") > 25 {
		t.Fatalf("the word is far from the start: %q", got)
	}
	if short := "a needle"; around(short, "needle", 20, 80) != short {
		t.Fatal("changed short text")
	}
	if !strings.Contains(around(strings.Repeat("a", 300)+"needle", "needle", 20, 80), "needle") {
		t.Fatal("lost a word at the end")
	}
	if !strings.Contains(around(strings.Repeat("ა", 300)+"needle"+strings.Repeat("ბ", 300), "needle", 20, 80), "needle") {
		t.Fatal("lost a word after multi-byte text")
	}
}

func TestAroundStartsAtAWholeWord(t *testing.T) {
	got := around("first reduced-motion benchmark and then the needle is here", "needle", 20, 80)
	if !strings.HasPrefix(got, "…and then the needle") && !strings.HasPrefix(got, "…then the needle") {
		t.Fatalf("got %q", got)
	}
}

func TestAroundPrefersTheStartOfAWord(t *testing.T) {
	got := around("Japanese "+strings.Repeat("x ", 100)+"panes", "panes", 5, 20)
	if !strings.HasSuffix(got, "panes") {
		t.Fatalf("got %q", got)
	}
}

func TestMarks(t *testing.T) {
	got := marks("a Word", []string{"word"})
	want := []bool{false, false, true, true, true, true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v", got)
	}
	if got := marks("ა word", []string{"word"}); len(got) != 6 || !got[2] {
		t.Fatalf("multi-byte text: %v", got)
	}
}

func TestWrap(t *testing.T) {
	if got := strings.Join(wrap("one two three", 7), "|"); got != "one two|three" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(wrap("abcdefgh", 3), "|"); got != "abc|def|gh" {
		t.Fatalf("a long word: %q", got)
	}
	if got := strings.Join(wrap("ა ბ გ", 3), "|"); got != "ა ბ|გ" {
		t.Fatalf("multi-byte text: %q", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := strings.Join(lastLines("a\nb\nc\n", 2), "|"); got != "b|c" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(lastLines("a\n", 5), "|"); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestPathFromFileURL(t *testing.T) {
	if got := pathFromFileURL("file://host.local/Users/me/My%20Code"); got != "/Users/me/My Code" {
		t.Fatalf("got %q", got)
	}
	if got := pathFromFileURL(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestANameMatchesWithLettersLeftOut(t *testing.T) {
	for _, c := range []struct{ query, name string }{
		{"tlbr", "tilebar"},
		{"jl", "jolt-landing"},
		{"splt", "Split pane"},
	} {
		results := search([]pane{{Block: "a", Name: c.name}}, c.query)
		if len(results) != 1 || results[0].Kind != kindPane || results[0].Heading() != "Panes" {
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
		for _, r := range search([]pane{{Block: "a", Name: c.name}}, c.query) {
			if r.Letters != nil {
				t.Errorf("%s in %s: got %+v", c.query, c.name, r)
			}
		}
	}
}

func TestAWholeNameComesBeforeLettersLeftOut(t *testing.T) {
	panes := []pane{{Block: "letters", Name: "tilebar"}, {Block: "whole", Name: "tlbr tools"}}
	if got := strings.Join(kinds(search(panes, "tlbr")), " "); got != "pane:whole pane:letters" {
		t.Fatalf("got %v", got)
	}
}

func TestWordsInThePaneBeatLettersOfItsName(t *testing.T) {
	panes := []pane{{Block: "letters", Name: "tilebar"}, talking(t, "talk", "x", said("user", "the tlbr file"))}
	results := search(panes, "tlbr")
	if got := strings.Join(kinds(results), " "); got != "pane:letters said:talk" {
		t.Fatalf("got %v", got)
	}
	both := talking(t, "both", "tilebar", said("user", "the tlbr file"))
	if got := search([]pane{both}, "tlbr")[0]; got.Kind != kindSaid || got.Letters != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestAnEmptyQueryShowsRecentConversationsFirst(t *testing.T) {
	now := time.Now()
	old, recent := pane{Block: "old", Name: "o", Conversation: true, Active: now.Add(-time.Hour)}, pane{Block: "new", Name: "n", Conversation: true, Active: now}
	here := pane{Block: "here", Name: "h", Conversation: true, Active: now.Add(time.Minute), Here: true}
	shell := pane{Block: "shell", Name: "s"}
	results := search([]pane{shell, old, here, recent}, "")
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
	var panes []pane
	for i := range maxRecent + 3 {
		panes = append(panes, pane{Block: fmt.Sprint(i), Name: "x", Conversation: true, Active: time.Now().Add(time.Duration(i) * time.Minute)})
	}
	results := search(panes, "")
	if results[0].Pane.Block != fmt.Sprint(maxRecent+2) || results[maxRecent-1].Heading() != "Recent conversations" || results[maxRecent].Heading() != "Panes" {
		t.Fatalf("got %+v", results[maxRecent])
	}
}

func TestAge(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{time.Second, "now"}, {5 * time.Minute, "5m"}, {3 * time.Hour, "3h"}, {50 * time.Hour, "2d"}} {
		if got := age(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v: got %q", c.ago, got)
		}
	}
}
