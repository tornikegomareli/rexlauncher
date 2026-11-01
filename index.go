package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A Claude Code transcript is mostly tool output and copies of API requests: of
// 2.3 GB measured on a real machine, 6.7 MB was text that people read. So the
// picker searches an index with only that text, and each open only reads what
// the transcript added since the last open.

// indexMaxAge is how long the index of a conversation stays after its last use.
// It holds a copy of the conversation, so it should not stay for ever.
const indexMaxAge = 30 * 24 * time.Hour

// tmpPrefix starts the name of an index that is still being written.
const tmpPrefix = ".writing-"

type message struct {
	Role string
	At   string
	Text string
}

func (m message) line() string {
	return m.Role + "\t" + m.At + "\t" + flatten(m.Text) + "\n"
}

func parseMessage(line string) message {
	role, rest, _ := strings.Cut(line, "\t")
	at, text, _ := strings.Cut(rest, "\t")
	return message{Role: role, At: at, Text: text}
}

// flatten keeps a message on one line.
func flatten(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	}), " ")
}

// extractMessage returns the text a person reads in one transcript line. Tool
// calls, tool output and thinking are left out, and so are Claude Code's own
// messages: those it marks, and those it wraps in a tag such as <command-name>.
func extractMessage(line []byte) (message, bool) {
	// Most bytes are in lines of other types, so do not decode those.
	if !bytes.Contains(line, []byte(`"type":"user"`)) && !bytes.Contains(line, []byte(`"type":"assistant"`)) {
		return message{}, false
	}
	var entry struct {
		Type             string `json:"type"`
		Timestamp        string `json:"timestamp"`
		IsMeta           bool   `json:"isMeta"`
		IsSidechain      bool   `json:"isSidechain"`
		IsCompactSummary bool   `json:"isCompactSummary"`
		Message          struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &entry); err != nil {
		return message{}, false
	}
	if entry.Type != "user" && entry.Type != "assistant" {
		return message{}, false
	}
	if entry.IsMeta || entry.IsSidechain || entry.IsCompactSummary {
		return message{}, false
	}
	var parts []string
	var whole string
	if json.Unmarshal(entry.Message.Content, &whole) == nil {
		parts = append(parts, whole)
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(entry.Message.Content, &blocks) != nil {
			return message{}, false
		}
		for _, block := range blocks {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" || (entry.Type == "user" && strings.HasPrefix(text, "<")) {
		return message{}, false
	}
	return message{Role: entry.Type, At: entry.Timestamp, Text: text}, true
}

// updateIndex brings the index of one conversation up to date and returns its path.
func updateIndex(dir, id, transcript string) (string, error) {
	path := filepath.Join(dir, id+".txt")
	offsetPath := filepath.Join(dir, id+".offset")

	in, err := os.Open(transcript)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}

	offset, size := readOffset(offsetPath)
	var index bytes.Buffer
	kept, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// The transcript was replaced, or the index is not what the offset describes.
	if offset > info.Size() || int64(len(kept)) < size {
		offset, size = 0, 0
	}
	index.Write(kept[:size]) // More than size is what a killed process wrote last.
	if _, err := in.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}

	before := index.Len()
	reader := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break // A line without its end is still being written. It comes next time.
		}
		if err != nil {
			return "", err
		}
		offset += int64(len(line))
		if m, ok := extractMessage(line); ok {
			index.WriteString(m.line())
		}
	}
	// A rename, because closing the picker can end this process at any moment. The
	// offset names the size of the index it describes, so it stays true until it is
	// saved too.
	if index.Len() > before || size == 0 {
		if err := writeFileAtomic(path, index.Bytes()); err != nil {
			return "", err
		}
	}
	now := time.Now()
	os.Chtimes(path, now, now) // Keeps it from pruneIndex while it is in use.
	return path, writeFileAtomic(offsetPath, fmt.Appendf(nil, "%d %d", offset, index.Len()))
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // No effect once the rename has happened.
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// readOffset returns how far the transcript was read, and the size of the index
// at that point. Anything it cannot read means to start again.
func readOffset(path string) (offset, size int64) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	if _, err := fmt.Sscanf(string(data), "%d %d", &offset, &size); err != nil {
		return 0, 0
	}
	return offset, size
}

// pruneIndex deletes the indexes of conversations not used for indexMaxAge.
func pruneIndex(dir string) {
	// What a process that was ended left behind. Another picker can be writing, so
	// only old ones go.
	temporary, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	for _, path := range temporary {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(path)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || time.Since(info.ModTime()) < indexMaxAge {
			continue
		}
		os.Remove(path)
		os.Remove(strings.TrimSuffix(path, ".txt") + ".offset")
	}
}
