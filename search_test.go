package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func kinds(results []result) []string {
	var out []string
	for _, r := range results {
		out = append(out, r.Kind+":"+r.Pane.Block)
	}
	return out
}

func TestEmptyQueryListsEveryPaneOnly(t *testing.T) {
	panes := []pane{{Block: "a", Name: "one", Index: writeLines(t, said("user", "hello"))}, {Block: "b", Name: "two"}}
	got := kinds(search(panes, "  "))
	if strings.Join(got, " ") != "pane:a pane:b" {
		t.Fatalf("got %v", got)
	}
}

func TestPanesComeBeforeWordsAndWordsBeforeScreens(t *testing.T) {
	panes := []pane{
		{Block: "screen", Name: "shell", Screen: writeLines(t, "flock failed")},
		{Block: "talk", Name: "shell", Index: writeLines(t, said("assistant", "the flock call"))},
		{Block: "named", Name: "flock work"},
	}
	got := kinds(search(panes, "flock"))
	if strings.Join(got, " ") != "pane:named said:talk screen:screen" {
		t.Fatalf("got %v", got)
	}
}

func TestAPaneHasOneRowHoweverManyMessagesMatch(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, said("user", fmt.Sprintf("word %d", i)))
	}
	results := search([]pane{{Block: "a", Name: "x", Index: writeLines(t, lines...)}}, "word")
	if len(results) != 1 || results[0].Matches != 40 {
		t.Fatalf("got %+v", results)
	}
	if !strings.Contains(results[0].Display, "40 matches") {
		t.Errorf("no count in %q", results[0].Display)
	}
}

func TestARowNamesThePaneBeforeTheMatch(t *testing.T) {
	p := pane{SessionLabel: "work", WindowLabel: "main", Name: "Fix the login", Index: writeLines(t, said("user", "a word"))}
	first, second, ok := strings.Cut(search([]pane{p}, "word")[0].Display, "\n")
	if !ok || !strings.Contains(first, "Fix the login") || !strings.Contains(first, "work") {
		t.Fatalf("first line: %q", first)
	}
	if !strings.Contains(second, hit+"word"+reset) || !strings.Contains(second, "you: ") {
		t.Fatalf("second line: %q", second)
	}
}

func TestARowOfAClaudeMessageHasNoLabel(t *testing.T) {
	p := pane{Name: "x", Index: writeLines(t, said("assistant", "a word"))}
	if display := search([]pane{p}, "word")[0].Display; strings.Contains(display, "claude") {
		t.Fatalf("got %q", display)
	}
}

func TestARowWithoutAMatchInTextShowsTheFolder(t *testing.T) {
	p := pane{Name: "work", Cwd: "/somewhere/code"}
	for _, query := range []string{"", "work"} {
		if display := search([]pane{p}, query)[0].Display; !strings.Contains(display, "\n"+dim+"/somewhere/code") {
			t.Errorf("%q: got %q", query, display)
		}
	}
}

func TestARowDoesNotRepeatASessionNamedLikeItsPane(t *testing.T) {
	display := search([]pane{{SessionLabel: "Panes", WindowLabel: "Panes", Name: "panes"}}, "")[0].Display
	first, _, _ := strings.Cut(display, "\n")
	if strings.Count(strings.ToLower(first), "panes") != 1 {
		t.Fatalf("got %q", first)
	}
}

func TestAPaneWithoutANameUsesItsWindow(t *testing.T) {
	display := search([]pane{{SessionLabel: "s", WindowLabel: "main"}}, "")[0].Display
	if first, _, _ := strings.Cut(display, "\n"); !strings.Contains(first, "main") {
		t.Fatalf("got %q", first)
	}
}

func TestAMatchAtTheStartOfAWordBeatsAMatchInsideOne(t *testing.T) {
	panes := []pane{
		{Block: "inside", Name: "x", Index: writeLines(t, said("user", "I read Japanese"))},
		{Block: "start", Name: "y", Screen: writeLines(t, "split panes here")},
	}
	if got := strings.Join(kinds(search(panes, "panes")), " "); got != "screen:start said:inside" {
		t.Fatalf("got %v", got)
	}
	// The word a person is still typing counts too.
	if got := strings.Join(kinds(search(panes, "pane")), " "); !strings.HasPrefix(got, "screen:start") {
		t.Fatalf("got %v", got)
	}
}

func TestARowShowsTheBestMatchOfItsPane(t *testing.T) {
	p := pane{Name: "x", Index: writeLines(t, said("user", "older panes"), said("user", "newer panes"), said("user", "newest Japanese"))}
	if display := search([]pane{p}, "panes")[0].Display; !strings.Contains(display, "newer") {
		t.Fatalf("got %q", display)
	}
}

func TestTheNewestConversationComesFirst(t *testing.T) {
	line := func(at string) string { return message{Role: "user", At: at, Text: "word"}.line() }
	panes := []pane{
		{Block: "old", Name: "x", Index: writeLines(t, strings.TrimSuffix(line("2026-01-01T00:00:00Z"), "\n"))},
		{Block: "new", Name: "y", Index: writeLines(t, strings.TrimSuffix(line("2026-06-01T00:00:00Z"), "\n"))},
	}
	if got := strings.Join(kinds(search(panes, "word")), " "); got != "said:new said:old" {
		t.Fatalf("got %v", got)
	}
}

func TestSearchIgnoresCaseAndNeedsEveryWord(t *testing.T) {
	panes := []pane{{Block: "a", Name: "x", Index: writeLines(t, said("user", "Fix the FLOCK bug"), said("user", "flock only"))}}
	results := search(panes, "bug flock")
	if len(results) != 1 || results[0].Matches != 1 {
		t.Fatalf("got %+v", results)
	}
}

func TestSearchFindsAnyAlphabet(t *testing.T) {
	panes := []pane{{Block: "a", Name: "x", Index: writeLines(t, said("user", "გამარჯობა Мир"))}}
	for _, query := range []string{"გამარჯობა", "мир"} {
		if got := search(panes, query); len(got) != 1 {
			t.Errorf("%s: got %d results", query, len(got))
		}
	}
}

func TestIdenticalMessagesCountOnce(t *testing.T) {
	p := pane{Name: "x", Index: writeLines(t, said("user", "a word"), said("assistant", "other"), said("user", "a word"))}
	if got := search([]pane{p}, "word")[0].Matches; got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestMarkdownIsNotShownOrSearched(t *testing.T) {
	p := pane{Name: "x", Index: writeLines(t, said("assistant", "## Result **the** `word` [Image #9] here"))}
	display := search([]pane{p}, "word")[0].Display
	for _, noise := range []string{"**", "`", "##", "Image"} {
		if strings.Contains(display, noise) {
			t.Errorf("%q in %q", noise, display)
		}
	}
	if !strings.Contains(display, "Result the "+hit+"word"+reset) {
		t.Errorf("got %q", display)
	}
	if got := search([]pane{p}, "image"); len(got) != 0 {
		t.Errorf("found an image tag: %+v", got)
	}
}

func TestARowKeepsItsFields(t *testing.T) {
	p := pane{Session: "s", Window: "w", Block: "b", Name: "x", Index: writeLines(t, said("user", "a word"))}
	row := search([]pane{p}, "word")[0].row()
	fields := strings.Split(row, "\t")
	if len(fields) != 4 || fields[0] != "s" || fields[1] != "w" || fields[2] != "b" {
		t.Fatalf("got %q", fields)
	}
}

func TestDisplayHasNoControlCharactersFromText(t *testing.T) {
	p := pane{Name: "x", Index: writeLines(t, said("user", "a word \x1b[2J here"))}
	display := search([]pane{p}, "word")[0].Display
	if strings.Contains(display, "\x1b[2J") {
		t.Fatalf("got %q", display)
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

func TestHighlight(t *testing.T) {
	if got := highlight("a Word b", []string{"word"}); got != "a "+hit+"Word"+reset+" b" {
		t.Fatalf("got %q", got)
	}
	if got := highlight("plain", []string{"none"}); got != "plain" {
		t.Fatalf("got %q", got)
	}
	if got := highlight("word", []string{"word", "or"}); got != hit+"word"+reset {
		t.Fatalf("overlap: %q", got)
	}
}

func TestPreviewListsOnlyTheMatchesOfThePaneBestFirst(t *testing.T) {
	index := writeLines(t, said("user", "the old word"), said("assistant", "other"), said("assistant", "the new word"), said("user", "a sword"))
	got := preview([]pane{{Block: "b", Name: "paneName", Cwd: "/folder", Index: index}}, "b", "word", 60, 20)
	newest, oldest, inside := strings.Index(got, "the new"), strings.Index(got, "the old"), strings.Index(got, "a s")
	if newest < 0 || oldest < newest || inside < oldest {
		t.Errorf("order in %q", got)
	}
	for _, unwanted := range []string{"other", "paneName", "/folder"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q in %q", unwanted, got)
		}
	}
	if !strings.Contains(got, dim+"you: "+reset+"the old "+hit+"word"+reset) {
		t.Errorf("no label in %q", got)
	}
}

func TestPreviewShowsALimitedNumberOfMatches(t *testing.T) {
	var lines []string
	for i := range maxInPreview + 5 {
		lines = append(lines, said("user", fmt.Sprintf("word %d", i)))
	}
	got := preview([]pane{{Block: "b", Index: writeLines(t, lines...)}}, "b", "word", 60, 20)
	if count := strings.Count(got, "you: "); count != maxInPreview {
		t.Fatalf("%d matches", count)
	}
}

func TestPreviewWrapsAtWords(t *testing.T) {
	index := writeLines(t, said("assistant", "the word and some more text that goes on and on"))
	got := preview([]pane{{Block: "b", Index: index}}, "b", "word", 11, 20)
	if !strings.Contains(got, "\nand some\n") {
		t.Fatalf("got %q", got)
	}
}

func TestWrap(t *testing.T) {
	got := strings.Join(wrap("one two three", 7), "|")
	if got != "one two|three" {
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
	if got := lastLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("a\n", 5); got != "a" {
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

func TestPreviewOfNoRowIsEmpty(t *testing.T) {
	if got := preview([]pane{{Block: "b", Name: "x"}}, "", "word", 60, 20); got != "" {
		t.Fatalf("got %q", got)
	}
}
