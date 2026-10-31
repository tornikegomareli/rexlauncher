package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

	offset := readOffset(offsetPath)
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	if offset > info.Size() {
		offset = 0 // The transcript was replaced, so its old offset means nothing.
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	if _, err := in.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	out, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()

	writer := bufio.NewWriter(out)
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
			if _, err := writer.WriteString(m.line()); err != nil {
				return "", err
			}
		}
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	now := time.Now()
	os.Chtimes(path, now, now) // Keeps it from pruneIndex while it is in use.
	return path, os.WriteFile(offsetPath, []byte(strconv.FormatInt(offset, 10)), 0o600)
}

func readOffset(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return offset
}

// pruneIndex deletes the indexes of conversations not used for indexMaxAge.
func pruneIndex(dir string) {
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
