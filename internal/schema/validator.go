package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates JSON data against JSON Schema documents.
type Validator struct{}

// NewValidator returns a ready-to-use Validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate compiles the given JSON Schema string and validates data against it.
func (v *Validator) Validate(data []byte, jsonSchema string) error {
	c := jsonschema.NewCompiler()

	var schemaDoc interface{}
	if err := json.Unmarshal([]byte(jsonSchema), &schemaDoc); err != nil {
		return fmt.Errorf("schema: decoding schema JSON: %w", err)
	}

	if err := c.AddResource("schema.json", schemaDoc); err != nil {
		return fmt.Errorf("schema: adding resource: %w", err)
	}

	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("schema: compiling schema: %w", err)
	}

	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("schema: decoding JSON data: %w", err)
	}

	if err := sch.Validate(decoded); err != nil {
		return fmt.Errorf("schema: validation failed: %w", err)
	}

	return nil
}

// ExtractJSON finds the first JSON object ({...}) or array ([...]) in raw text.
// This is a fallback for when LLM output contains extra text around JSON.
func ExtractJSON(raw string) (string, error) {
	// Try to find a JSON object first, then array.
	for _, pattern := range []struct {
		open  byte
		close byte
	}{
		{'{', '}'},
		{'[', ']'},
	} {
		start := strings.IndexByte(raw, pattern.open)
		if start == -1 {
			continue
		}

		// Walk forward counting brace/bracket depth to find the matching close.
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(raw); i++ {
			ch := raw[i]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' && inString {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if ch == pattern.open {
				depth++
			} else if ch == pattern.close {
				depth--
				if depth == 0 {
					candidate := raw[start : i+1]
					// Validate that it's actually valid JSON.
					if json.Valid([]byte(candidate)) {
						// Pretty-print for consistency.
						var buf bytes.Buffer
						if err := json.Compact(&buf, []byte(candidate)); err == nil {
							return buf.String(), nil
						}
						return candidate, nil
					}
				}
			}
		}
	}

	// Fallback: try regex for simple cases.
	re := regexp.MustCompile(`(?s)(\{.*\}|\[.*\])`)
	match := re.FindString(raw)
	if match != "" && json.Valid([]byte(match)) {
		return match, nil
	}

	return "", fmt.Errorf("schema: no valid JSON found in input")
}

// ValidationResult holds the outcome of a validation attempt, including
// structured feedback suitable for re-prompting an LLM.
type ValidationResult struct {
	Valid    bool   // Whether the data passed validation.
	Output   string // The (possibly extracted) JSON output.
	Feedback string // Human-readable description of validation errors.
	Attempt  int    // Which attempt number this was (1-based).
}

// ValidateWithFeedback attempts validation and returns structured feedback
// on failure. This feedback can be used to construct a re-prompt.
func (v *Validator) ValidateWithFeedback(data []byte, jsonSchema string) *ValidationResult {
	// Step 1: Try to extract JSON if raw text.
	extracted, err := ExtractJSON(string(data))
	if err != nil {
		return &ValidationResult{
			Valid:    false,
			Output:   string(data),
			Feedback: fmt.Sprintf("Could not find valid JSON in the output. Error: %s. Please respond with only valid JSON matching the schema.", err.Error()),
		}
	}

	// Step 2: Validate against schema.
	if err := v.Validate([]byte(extracted), jsonSchema); err != nil {
		return &ValidationResult{
			Valid:    false,
			Output:   extracted,
			Feedback: fmt.Sprintf("JSON was found but does not match the required schema. Error: %s. Please fix the JSON to match the schema.", err.Error()),
		}
	}

	return &ValidationResult{
		Valid:  true,
		Output: extracted,
	}
}

// BuildRetryPrompt creates a prompt that asks the LLM to fix its output
// based on validation feedback.
func BuildRetryPrompt(originalPrompt string, failedOutput string, feedback string, schema string) string {
	return fmt.Sprintf(`Your previous response did not produce valid structured output.

Original request: %s

Your previous output:
%s

Validation error: %s

Required JSON Schema:
%s

Please respond with ONLY valid JSON that matches the schema above. Do not include any explanation or markdown formatting.`, originalPrompt, failedOutput, feedback, schema)
}
