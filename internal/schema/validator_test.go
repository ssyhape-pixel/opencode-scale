package schema

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Validator.Validate tests
// ---------------------------------------------------------------------------

func TestValidate_ValidData(t *testing.T) {
	v := NewValidator()
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age":  {"type": "integer"}
		},
		"required": ["name"]
	}`
	data := []byte(`{"name": "Alice", "age": 30}`)

	if err := v.Validate(data, schema); err != nil {
		t.Fatalf("expected valid data to pass validation, got error: %v", err)
	}
}

func TestValidate_InvalidData(t *testing.T) {
	v := NewValidator()
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age":  {"type": "integer"}
		},
		"required": ["name", "age"]
	}`
	// Missing the required "age" field.
	data := []byte(`{"name": "Alice"}`)

	err := v.Validate(data, schema)
	if err == nil {
		t.Fatal("expected validation error for missing required field, got nil")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected 'validation failed' in error, got: %v", err)
	}
}

func TestValidate_InvalidSchema(t *testing.T) {
	v := NewValidator()
	// Malformed JSON schema string (not valid JSON).
	schema := `{not valid json`
	data := []byte(`{"key": "value"}`)

	err := v.Validate(data, schema)
	if err == nil {
		t.Fatal("expected error for invalid schema, got nil")
	}
	if !strings.Contains(err.Error(), "schema:") {
		t.Fatalf("expected error prefixed with 'schema:', got: %v", err)
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	v := NewValidator()
	schema := `{"type": "object"}`
	data := []byte(`not json at all`)

	err := v.Validate(data, schema)
	if err == nil {
		t.Fatal("expected error for invalid JSON data, got nil")
	}
	if !strings.Contains(err.Error(), "decoding JSON data") {
		t.Fatalf("expected 'decoding JSON data' in error, got: %v", err)
	}
}

func TestValidate_ArrayData(t *testing.T) {
	v := NewValidator()
	schema := `{
		"type": "array",
		"items": {"type": "integer"}
	}`
	data := []byte(`[1, 2, 3]`)

	if err := v.Validate(data, schema); err != nil {
		t.Fatalf("expected valid array to pass validation, got error: %v", err)
	}

	// An array with a non-integer element should fail.
	badData := []byte(`[1, "two", 3]`)
	if err := v.Validate(badData, schema); err == nil {
		t.Fatal("expected validation error for array with wrong item type, got nil")
	}
}

// ---------------------------------------------------------------------------
// ExtractJSON tests
// ---------------------------------------------------------------------------

func TestExtractJSON_SimpleObject(t *testing.T) {
	input := `Here is the result: {"key":"value"} and some trailing text`
	got, err := ExtractJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"key":"value"}`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestExtractJSON_NestedObject(t *testing.T) {
	input := `Here is the result: {"a":{"b":1}} done`
	got, err := ExtractJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"a":{"b":1}}`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestExtractJSON_Array(t *testing.T) {
	input := `the values are [1,2,3] ok`
	got, err := ExtractJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `[1,2,3]`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	input := `there is absolutely no json here`
	_, err := ExtractJSON(input)
	if err == nil {
		t.Fatal("expected error when no JSON is present, got nil")
	}
	if !strings.Contains(err.Error(), "no valid JSON found") {
		t.Fatalf("expected 'no valid JSON found' in error, got: %v", err)
	}
}

func TestExtractJSON_EscapedQuotes(t *testing.T) {
	input := `output: {"msg":"say \"hello\""} end`
	got, err := ExtractJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"msg":"say \"hello\""}`
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestExtractJSON_MultipleObjects(t *testing.T) {
	input := `first: {"a":1} second: {"b":2}`
	got, err := ExtractJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the first valid JSON object found.
	expected := `{"a":1}`
	if got != expected {
		t.Fatalf("expected first object %q, got %q", expected, got)
	}
}
