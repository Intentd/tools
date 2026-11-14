package structured

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/intentd/tools/fileio"
	"gopkg.in/yaml.v3"
)

// Unmarshal parses YAML or JSON into out.
// Unknown fields are allowed.
func Unmarshal(data []byte, out any) error {
	return unmarshalYAML(data, out, false)
}

// StrictUnmarshal parses YAML or JSON into out.
// Unknown fields are rejected.
func StrictUnmarshal(data []byte, out any) error {
	return unmarshalYAML(data, out, true)
}

// unmarshal detects the input format and decodes it.
// Strict mode rejects unknown fields for both JSON and YAML.
func unmarshal(data []byte, out any, strict bool) error {
	data = bytes.TrimSpace(data)

	if len(data) == 0 {
		return errors.New("empty input")
	}

	// JSON objects and arrays start with { or [.
	if data[0] == '{' || data[0] == '[' {
		return unmarshalJSON(data, out, strict)
	}

	return unmarshalYAML(data, out, strict)
}

// unmarshalJSON decodes JSON and optionally rejects unknown fields.
func unmarshalJSON(data []byte, out any, strict bool) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	if strict {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Reject multiple JSON values and malformed trailing content.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("invalid JSON: multiple values")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return nil
}

// unmarshalYAML decodes YAML and optionally rejects unknown fields.
func unmarshalYAML(data []byte, out any, strict bool) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))

	if strict {
		dec.KnownFields(true)
	}

	if err := dec.Decode(out); err != nil {
		return errors.New(FormatYAMLError(err, data))
	}

	// Reject multiple YAML documents.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("invalid YAML: multiple documents")
		}
		return errors.New(FormatYAMLError(err, data))
	}

	return nil
}

// FormatYAMLError converts YAML errors into user-friendly messages
// with the relevant source line when available.
func FormatYAMLError(err error, data []byte) string {
	if err == nil {
		return ""
	}

	var errorLines []string
	lines := strings.Split(err.Error(), "\n")

	// Extract context lines for each error
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if !strings.Contains(line, "line ") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		lineInfo := strings.TrimSpace(parts[0])
		errorMsg := strings.TrimSpace(parts[1])

		var lineNum int
		if _, err := fmt.Sscanf(lineInfo, "line %d", &lineNum); err != nil {
			continue
		}

		contextLine := getLineFromData(data, lineNum)

		// Unknown field errors.
		if strings.HasPrefix(errorMsg, "field ") {
			fieldWords := strings.Fields(errorMsg)
			if len(fieldWords) >= 2 {
				errorLines = append(errorLines,
					fmt.Sprintf("• %s: additional properties '%s' not allowed",
						contextLine,
						fieldWords[1]))
				continue
			}
		}

		// Generic YAML errors.
		humanMsg := translateYAMLErrorMsg(errorMsg)

		errorLines = append(errorLines,
			fmt.Sprintf("• %s: %s", contextLine, humanMsg))
	}

	if len(errorLines) == 0 {
		return err.Error()
	}

	return "Validation failed:\n" + strings.Join(errorLines, "\n")
}

// translateYAMLErrorMsg converts raw yaml.v3 type mismatch messages
// into user-friendly text.
func translateYAMLErrorMsg(msg string) string {
	if !strings.HasPrefix(msg, "cannot unmarshal") {
		// Custom error from an UnmarshalYAML implementation — show as-is.
		return msg
	}

	// yaml.v3 format: "cannot unmarshal !!TAG [VALUE ]into GOTYPE"
	// The VALUE (e.g. `secretName`) may or may not be present between TAG and "into".
	// Find the word after " into " to get the Go type.
	intoIdx := strings.Index(msg, " into ")
	if intoIdx < 0 {
		return msg
	}

	goType := strings.TrimSpace(msg[intoIdx+6:])

	// Extract YAML tag (first !!WORD after "cannot unmarshal ")
	tagIdx := strings.Index(msg, "!!")
	yamlTag := ""

	if tagIdx >= 0 {
		rest := msg[tagIdx:]
		if sp := strings.IndexByte(rest, ' '); sp > 0 {
			yamlTag = rest[:sp]
		} else {
			yamlTag = rest
		}
	}

	got := yamlTagFriendly(yamlTag)
	want := goTypeFriendly(goType)

	return fmt.Sprintf("expected %s, got %s", want, got)
}

func yamlTagFriendly(tag string) string {
	switch tag {
	case "!!map":
		return "an object (key: value pairs)"
	case "!!seq":
		return "a list (items starting with -)"
	case "!!str":
		return "a string"
	case "!!int":
		return "a number"
	case "!!float":
		return "a decimal number"
	case "!!bool":
		return "true or false"
	case "!!null":
		return "null"
	default:
		return tag
	}
}

func goTypeFriendly(goType string) string {
	// Strip pointer prefix
	goType = strings.TrimPrefix(goType, "*")

	// Slice types → "a list"
	if strings.HasPrefix(goType, "[]") {
		return "a list"
	}

	// Primitive types
	switch goType {
	case "string":
		return "a string"
	case "int", "int32", "int64", "uint", "uint32", "uint64":
		return "a number"
	case "float32", "float64":
		return "a decimal number"
	case "bool":
		return "true or false"
	}

	// Anything else (struct / map type from a package) → "an object"
	return "an object"
}

func getLineFromData(data []byte, lineNum int) string {
	if lineNum <= 0 {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if lineNum-1 < len(lines) {
		return strings.TrimSpace(lines[lineNum-1])
	}

	return ""
}

// FormatYAMLFile reads path, parses it into a yaml.Node and writes it back
// with consistent indentation and preserved comments and ordering.
func FormatYAMLFile(path string) error {
	data, err := fileio.ReadLocal(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("close encoder: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// WriteFileAndFormat writes data to path and then formats the YAML file.
func WriteFileAndFormat(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := FormatYAMLFile(path); err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}

	return nil
}
