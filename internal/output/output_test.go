package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

func TestJSONRetainsNativePayloadAndNumbers(t *testing.T) {
	value := map[string]any{
		"id":     json.Number("9007199254740993"),
		"amount": json.Number("12.3400"),
		"nested": []any{map[string]any{"url": "https://example.test?a=1&b=2", "name": "<invoice>"}},
	}
	var out bytes.Buffer
	if err := Write(&out, "json", value); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("invalid JSON: %s", out.String())
	}
	for _, expected := range []string{`9007199254740993`, `12.3400`, `<invoice>`, `a=1&b=2`} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("output %q missing %q", out.String(), expected)
		}
	}
	var decoded map[string]any
	decoder := json.NewDecoder(&out)
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(value) || decoded["id"] != value["id"] || decoded["amount"] != value["amount"] {
		t.Fatalf("native payload changed: %#v", decoded)
	}
}

func TestTextObjectOrderIsDeterministic(t *testing.T) {
	value := map[string]any{"z": 2, "a": "customer", "id": 123}
	for range 20 {
		var out bytes.Buffer
		if err := Write(&out, "text", value); err != nil {
			t.Fatal(err)
		}
		if expected := "id: 123\na: customer\nz: 2\n"; out.String() != expected {
			t.Fatalf("got %q, want %q", out.String(), expected)
		}
	}
}

func TestScalarsAndEmptyResults(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"null", nil, "null\n"},
		{"string", "done", "done\n"},
		{"boolean", true, "true\n"},
		{"number", json.Number("9007199254740993"), "9007199254740993\n"},
		{"empty list", []any{}, "No results.\n"},
		{"empty object", map[string]any{}, "{}\n"},
		{"scalar list", []any{1, "two", false}, "1\ntwo\nfalse\n"},
		{"empty object list", []map[string]any{{}, {}}, "{}\n{}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Write(&out, "text", tt.value); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("got %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestTextTableIncludesNestedValuesAndMissingColumns(t *testing.T) {
	value := []map[string]any{
		{"id": 2, "name": "Alice", "tags": []string{"one"}},
		{"id": 3, "name": "Bob"},
	}
	var out bytes.Buffer
	if err := Write(&out, "text", value); err != nil {
		t.Fatal(err)
	}
	want := "id  name   tags\n2   Alice  [\"one\"]\n3   Bob    \n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestTextTableCapsColumnsAndWidth(t *testing.T) {
	value := []map[string]any{{
		"id": 1, "a": strings.Repeat("á", 100), "b": 2, "c": 3,
		"d": 4, "e": 5, "f": 6, "nested": map[string]any{"secret": 1},
	}}
	var out bytes.Buffer
	if err := Write(&out, "text", value); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "id  a") || strings.Contains(out.String(), "nested") {
		t.Fatalf("unexpected columns: %s", out.String())
	}
	if !strings.Contains(out.String(), "...") || !strings.Contains(out.String(), "use --output json") {
		t.Fatalf("missing truncation indication: %s", out.String())
	}
	if strings.Contains(out.String(), "�") {
		t.Fatalf("truncation split a UTF-8 rune: %s", out.String())
	}
}

func TestLLMRetainsAllNestedDataAndUsesSafeFence(t *testing.T) {
	value := map[string]any{
		"description": "```\nnot a closing fence",
		"items":       []any{map[string]any{"id": json.Number("9007199254740993"), "details": []any{1, 2, "three"}}},
	}
	var out bytes.Buffer
	if err := Write(&out, "llm", value); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "# Result\n\n````json\n") || !strings.HasSuffix(out.String(), "````\n") {
		t.Fatalf("unexpected Markdown fence: %s", out.String())
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(out.String(), "# Result\n\n````json\n"), "````\n")
	if !json.Valid([]byte(payload)) || !strings.Contains(payload, "9007199254740993") || !strings.Contains(payload, `"three"`) {
		t.Fatalf("incomplete LLM data: %s", payload)
	}
}

func TestTerminalControlsAreEscaped(t *testing.T) {
	value := map[string]any{"\x1b[2Jkey": "hello\x1b[31m\r\n\t\x00\u009b\u202e", "nested": []string{"\x07\u009d"}}
	for _, format := range []string{"text", "llm"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			if err := Write(&out, format, value); err != nil {
				t.Fatal(err)
			}
			for _, control := range []string{"\x1b", "\x00", "\x07", "\r", "\u009b", "\u009d", "\u202e"} {
				if strings.Contains(out.String(), control) {
					t.Errorf("output contains terminal control %q: %q", control, out.String())
				}
			}
			if !strings.Contains(out.String(), `\u001b`) || !strings.Contains(out.String(), `\u009b`) {
				t.Fatalf("missing escaped controls: %q", out.String())
			}
		})
	}
}

func TestTypedValues(t *testing.T) {
	type invoice struct {
		ID       int    `json:"id"`
		Customer string `json:"customer"`
	}
	var out bytes.Buffer
	if err := Write(&out, "text", invoice{ID: 1, Customer: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "id: 1\ncustomer: Alice\n" {
		t.Fatalf("unexpected struct output: %q", out.String())
	}
}

func TestInvalidInputReturnsErrorBeforeWriting(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, format := range []string{"text", "json", "llm", "invalid"} {
		for _, value := range []any{func() {}, cycle} {
			var out bytes.Buffer
			if err := Write(&out, format, value); err == nil {
				t.Errorf("expected error for %s %T", format, value)
			}
			if out.Len() != 0 {
				t.Errorf("wrote output for invalid value: %q", out.String())
			}
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

type shortWriter struct{}

func (shortWriter) Write(value []byte) (int, error) { return len(value) / 2, nil }

func TestWriteErrorsPropagate(t *testing.T) {
	for _, format := range []string{"text", "json", "llm"} {
		if err := Write(failingWriter{}, format, map[string]any{"id": 1}); !errors.Is(err, syscall.EPIPE) {
			t.Errorf("%s: got %v, want EPIPE", format, err)
		}
		if err := Write(shortWriter{}, format, map[string]any{"id": 1}); !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("%s: got %v, want ErrShortWrite", format, err)
		}
	}
}
