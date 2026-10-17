// Package output renders API payloads for people and programs.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxColumns = 6
	maxWidth   = 28
)

// Write renders value without an envelope or status messages. JSON and LLM output
// retain the entire payload; text output favors a compact terminal display.
func Write(w io.Writer, format string, value any) error {
	if format != "text" && format != "llm" && format != "json" {
		return fmt.Errorf("unsupported output format %q: use text, json, or llm", format)
	}

	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if format == "json" {
		return writeString(w, encoded.String())
	}
	if format == "llm" {
		payload := escapeTerminalControls(encoded.String(), true)
		fence := strings.Repeat("`", max(3, longestBacktickRun(payload)+1))
		return writeString(w, fmt.Sprintf("# Result\n\n%sjson\n%s%s\n", fence, payload, fence))
	}

	var normalized any
	decoder := json.NewDecoder(&encoded)
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return err
	}
	var rendered strings.Builder
	writeText(&rendered, normalized)
	return writeString(w, rendered.String())
}

func writeString(w io.Writer, value string) error {
	n, err := io.WriteString(w, value)
	if err == nil && n != len(value) {
		return io.ErrShortWrite
	}
	return err
}

func writeText(w *strings.Builder, value any) {
	switch value := value.(type) {
	case map[string]any:
		if len(value) == 0 {
			w.WriteString("{}\n")
			return
		}
		for _, key := range sortedKeys(value) {
			fmt.Fprintf(w, "%s: %s\n", escapeTerminalControls(key, false), displayValue(value[key]))
		}
	case []any:
		if len(value) == 0 {
			w.WriteString("No results.\n")
			return
		}
		rows := make([]map[string]any, len(value))
		for i, entry := range value {
			row, ok := entry.(map[string]any)
			if !ok {
				for _, entry := range value {
					fmt.Fprintf(w, "%s\n", displayValue(entry))
				}
				return
			}
			rows[i] = row
		}
		writeTable(w, rows)
	default:
		fmt.Fprintf(w, "%s\n", displayValue(value))
	}
}

func writeTable(w *strings.Builder, rows []map[string]any) {
	fields := make(map[string]any)
	scalar := make(map[string]bool)
	for _, row := range rows {
		for key, value := range row {
			fields[key] = nil
			if _, exists := scalar[key]; !exists {
				scalar[key] = true
			}
			switch value.(type) {
			case map[string]any, []any:
				scalar[key] = false
			}
		}
	}
	keys := sortedKeys(fields)
	// Prefer identifiers and scalar columns; keep nested data visible when space permits.
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i] == "id" || keys[j] == "id" {
			return keys[i] == "id"
		}
		return scalar[keys[i]] && !scalar[keys[j]]
	})
	omitted := len(keys) > maxColumns
	if omitted {
		keys = keys[:maxColumns]
	}
	if len(keys) == 0 {
		for range rows {
			w.WriteString("{}\n")
		}
		return
	}
	widths := make([]int, len(keys))
	cells := make([][]string, len(rows)+1)
	cells[0] = make([]string, len(keys))
	for i, key := range keys {
		cells[0][i] = escapeTerminalControls(key, false)
		widths[i] = min(maxWidth, utf8.RuneCountInString(cells[0][i]))
	}
	for i, row := range rows {
		cells[i+1] = make([]string, len(keys))
		for j, key := range keys {
			if value, exists := row[key]; exists {
				cells[i+1][j] = displayValue(value)
			}
			widths[j] = min(maxWidth, max(widths[j], utf8.RuneCountInString(cells[i+1][j])))
		}
	}
	truncated := false
	for _, row := range cells {
		for i, cell := range row {
			if i > 0 {
				w.WriteString("  ")
			}
			runes := []rune(cell)
			if len(runes) > widths[i] {
				cell = string(runes[:max(0, widths[i]-3)]) + "..."
				truncated = true
			}
			w.WriteString(cell)
			if i < len(row)-1 {
				w.WriteString(strings.Repeat(" ", max(0, widths[i]-utf8.RuneCountInString(cell))))
			}
		}
		w.WriteByte('\n')
	}
	if omitted || truncated {
		w.WriteString("Some fields are shortened or omitted; use --output json for complete data.\n")
	}
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if key == "id" {
			copy(keys[1:i+1], keys[:i])
			keys[0] = key
			break
		}
	}
	return keys
}

func displayValue(value any) string {
	if text, ok := value.(string); ok {
		return escapeTerminalControls(text, false)
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	// Values have already passed JSON normalization, so encoding cannot fail.
	_ = encoder.Encode(value)
	return escapeTerminalControls(strings.TrimSuffix(encoded.String(), "\n"), false)
}

func escapeTerminalControls(value string, preserveWhitespace bool) string {
	var result strings.Builder
	for _, r := range value {
		if preserveWhitespace && (r == '\n' || r == '\t' || r == '\r') {
			result.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			switch r {
			case '\n':
				result.WriteString(`\n`)
			case '\r':
				result.WriteString(`\r`)
			case '\t':
				result.WriteString(`\t`)
			default:
				if r <= 0xffff {
					fmt.Fprintf(&result, `\u%04x`, r)
				} else {
					result.WriteString(strconv.QuoteRune(r))
				}
			}
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func longestBacktickRun(value string) int {
	longest, current := 0, 0
	for _, r := range value {
		if r == '`' {
			current++
			longest = max(longest, current)
		} else {
			current = 0
		}
	}
	return longest
}
