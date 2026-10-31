package main

import (
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

func TestSearchIgnoresCaseAndNeedsEveryWord(t *testing.T) {
	panes := []pane{{Block: "a", Name: "x", Index: writeLines(t, said("user", "Fix the FLOCK bug"), said("user", "flock only"))}}
	results := search(panes, "bug flock")
	if len(results) != 1 || results[0].Line != 1 {
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

func TestTheNewestMessageComesFirst(t *testing.T) {
	panes := []pane{{Block: "a", Name: "x", Index: writeLines(t, said("user", "word 1"), said("user", "other"), said("user", "word 3"))}}
	results := search(panes, "word")
	if len(results) != 2 || results[0].Line != 3 || results[1].Line != 1 {
		t.Fatalf("got %+v", results)
	}
}

func TestResultsAreLimited(t *testing.T) {
	var lines []string
	for range 100 {
		lines = append(lines, said("user", "word"))
	}
	var panes []pane
	for range 20 {
		panes = append(panes, pane{Name: "x", Index: writeLines(t, lines...)})
	}
	if got := len(search(panes[:1], "word")); got != maxPerPane {
		t.Errorf("one pane: %d results", got)
	}
	if got := len(search(panes, "word")); got != maxResults {
		t.Errorf("all panes: %d results", got)
	}
}

func TestARowKeepsItsFields(t *testing.T) {
	p := pane{Session: "s", Window: "w", Block: "b", Name: "x", Index: writeLines(t, said("user", "a word"))}
	row := search([]pane{p}, "word")[0].row()
	fields := strings.Split(row, "\t")
	if len(fields) != 6 || fields[0] != "s" || fields[1] != "w" || fields[2] != "b" || fields[3] != kindSaid || fields[4] != "1" {
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
	long := strings.Repeat("a", 300) + "needle" + strings.Repeat("b", 300)
	got := around(long, "needle")
	if !strings.Contains(got, "needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
	if short := "a needle"; around(short, "needle") != short {
		t.Fatal("changed short text")
	}
	atEnd := strings.Repeat("a", 300) + "needle"
	if !strings.Contains(around(atEnd, "needle"), "needle") {
		t.Fatal("lost a word at the end")
	}
	georgian := strings.Repeat("ა", 300) + "needle" + strings.Repeat("ბ", 300)
	if !strings.Contains(around(georgian, "needle"), "needle") {
		t.Fatal("lost a word after multi-byte text")
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

func TestPreviewOfAMessageShowsItsNeighbours(t *testing.T) {
	index := writeLines(t, said("user", "before"), said("assistant", "the word"), said("user", "after"))
	got := preview([]pane{{Block: "b", Name: "x", Index: index}}, "b", kindSaid, 2, "word")
	for _, want := range []string{"before", hit + "word" + reset, "after", "you", "claude"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
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
	if got := preview([]pane{{Block: "b", Name: "x"}}, "", "", 0, "word"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAResultShowsTheTextBeforeThePlace(t *testing.T) {
	p := pane{SessionLabel: "work", WindowLabel: "main", Name: "Fix the login", Index: writeLines(t, said("user", "a word"))}
	display := search([]pane{p}, "word")[0].Display
	text, place := strings.Index(display, "a "+hit+"word"), strings.Index(display, "work › Fix the login")
	if text < 0 || place < 0 || text > place {
		t.Fatalf("got %q", display)
	}
}
