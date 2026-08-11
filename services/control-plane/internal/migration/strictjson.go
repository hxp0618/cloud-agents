package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxJSONDepth      = 64
	maxJSONCollection = 16384
	maxJSONString     = 1 << 20
	maxJSONInteger    = uint64(9007199254740991)
)

// JSONValue is the deliberately small ADR-0009 JSON domain.
type JSONValue any

// ParseStrictJSON rejects duplicate decoded keys, BOM, trailing input, invalid
// UTF-8, non-integer numbers, negative integers, and resource-limit violations.
func ParseStrictJSON(data []byte) (JSONValue, error) {
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xef, 0xbb, 0xbf}) {
		return nil, fail(CodeInvalidJSON, "decode", "UTF-8 BOM is forbidden", nil)
	}
	if !utf8.Valid(data) {
		return nil, fail(CodeInvalidJSON, "decode", "input is not valid UTF-8", nil)
	}
	if err := validateJSONStringEscapes(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseJSONValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fail(CodeInvalidJSON, "decode", fmt.Sprintf("trailing token %v", token), nil)
		}
		return nil, fail(CodeInvalidJSON, "decode", "trailing invalid input", err)
	}
	return value, nil
}

func parseJSONValue(dec *json.Decoder, depth int) (JSONValue, error) {
	if depth > maxJSONDepth {
		return nil, fail(CodeInvalidJSON, "decode", "maximum JSON nesting exceeded", nil)
	}
	token, err := dec.Token()
	if err != nil {
		return nil, fail(CodeInvalidJSON, "decode", "invalid JSON token", err)
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]JSONValue)
			for dec.More() {
				if len(object) >= maxJSONCollection {
					return nil, fail(CodeInvalidJSON, "decode", "object member limit exceeded", nil)
				}
				keyToken, keyErr := dec.Token()
				if keyErr != nil {
					return nil, fail(CodeInvalidJSON, "decode", "invalid object key", keyErr)
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fail(CodeInvalidJSON, "decode", "object key is not a string", nil)
				}
				if len([]byte(key)) > maxJSONString || !validUnicodeScalarString(key) {
					return nil, fail(CodeInvalidJSON, "decode", "invalid or oversized object key", nil)
				}
				if _, duplicate := object[key]; duplicate {
					return nil, fail(CodeInvalidJSON, "decode", fmt.Sprintf("duplicate object key %q", key), nil)
				}
				child, childErr := parseJSONValue(dec, depth+1)
				if childErr != nil {
					return nil, childErr
				}
				object[key] = child
			}
			end, endErr := dec.Token()
			if endErr != nil || end != json.Delim('}') {
				return nil, fail(CodeInvalidJSON, "decode", "unterminated object", endErr)
			}
			return object, nil
		case '[':
			array := make([]JSONValue, 0)
			for dec.More() {
				if len(array) >= maxJSONCollection {
					return nil, fail(CodeInvalidJSON, "decode", "array entry limit exceeded", nil)
				}
				child, childErr := parseJSONValue(dec, depth+1)
				if childErr != nil {
					return nil, childErr
				}
				array = append(array, child)
			}
			end, endErr := dec.Token()
			if endErr != nil || end != json.Delim(']') {
				return nil, fail(CodeInvalidJSON, "decode", "unterminated array", endErr)
			}
			return array, nil
		default:
			return nil, fail(CodeInvalidJSON, "decode", "unexpected delimiter", nil)
		}
	case string:
		if len([]byte(value)) > maxJSONString || !validUnicodeScalarString(value) {
			return nil, fail(CodeInvalidJSON, "decode", "invalid or oversized string", nil)
		}
		return value, nil
	case json.Number:
		text := value.String()
		if text == "" || text == "-0" || text[0] == '-' || (len(text) > 1 && text[0] == '0') || strings.ContainsAny(text, ".eE+") {
			return nil, fail(CodeInvalidJSON, "decode", fmt.Sprintf("number %q is outside the unsigned integer profile", text), nil)
		}
		parsed, parseErr := strconv.ParseUint(text, 10, 64)
		if parseErr != nil || parsed > maxJSONInteger {
			return nil, fail(CodeInvalidJSON, "decode", fmt.Sprintf("integer %q exceeds the safe profile", text), parseErr)
		}
		return parsed, nil
	case bool, nil:
		return value, nil
	default:
		return nil, fail(CodeInvalidJSON, "decode", "unsupported JSON token", nil)
	}
}

func validUnicodeScalarString(value string) bool {
	for _, r := range value {
		if r >= 0xd800 && r <= 0xdfff {
			return false
		}
	}
	return utf8.ValidString(value)
}

// CanonicalJSON implements the ADR-0009 RFC 8785 subset. Numbers have already
// been restricted to unsigned safe integers, so ECMAScript number edge cases do not arise.
func CanonicalJSON(value JSONValue) ([]byte, error) {
	var output bytes.Buffer
	if err := writeCanonicalJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonicalJSON(output *bytes.Buffer, value JSONValue) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		writeCanonicalString(output, typed)
	case uint64:
		if typed > maxJSONInteger {
			return fail(CodeInvalidJSON, "canonicalize", "integer exceeds the safe profile", nil)
		}
		output.WriteString(strconv.FormatUint(typed, 10))
	case []JSONValue:
		output.WriteByte('[')
		for index, entry := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonicalJSON(output, entry); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]JSONValue:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			writeCanonicalString(output, key)
			output.WriteByte(':')
			if err := writeCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fail(CodeInvalidJSON, "canonicalize", fmt.Sprintf("unsupported value type %T", value), nil)
	}
	return nil
}

func utf16Less(left, right string) bool {
	l := utf16.Encode([]rune(left))
	r := utf16.Encode([]rune(right))
	for i := 0; i < len(l) && i < len(r); i++ {
		if l[i] != r[i] {
			return l[i] < r[i]
		}
	}
	return len(l) < len(r)
}

// DecodeStrict performs the lexical checks first, then rejects unknown fields.
func DecodeStrict(data []byte, target any) (JSONValue, error) {
	value, err := ParseStrictJSON(data)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return nil, fail(CodeInvalidJSON, "typed-decode", "typed JSON shape is invalid", err)
	}
	if err := validateRequiredFields(value, reflect.TypeOf(target)); err != nil {
		return nil, err
	}
	if token, err := dec.Token(); err != io.EOF {
		return nil, fail(CodeInvalidJSON, "typed-decode", fmt.Sprintf("unexpected trailing token %v", token), err)
	}
	return value, nil
}

func writeCanonicalString(output *bytes.Buffer, value string) {
	const hex = "0123456789abcdef"
	output.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(r)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if r < 0x20 {
				output.WriteString(`\u00`)
				output.WriteByte(hex[byte(r)>>4])
				output.WriteByte(hex[byte(r)&0x0f])
			} else {
				output.WriteRune(r)
			}
		}
	}
	output.WriteByte('"')
}

func validateJSONStringEscapes(data []byte) error {
	for cursor := 0; cursor < len(data); cursor++ {
		if data[cursor] != '"' {
			continue
		}
		for cursor++; cursor < len(data); cursor++ {
			if data[cursor] == '"' {
				break
			}
			if data[cursor] != '\\' {
				continue
			}
			cursor++
			if cursor >= len(data) {
				return fail(CodeInvalidJSON, "decode", "unterminated string escape", nil)
			}
			if data[cursor] != 'u' {
				continue
			}
			unit, ok := parseHexUTF16(data, cursor+1)
			if !ok {
				continue // encoding/json produces the stable syntax error.
			}
			cursor += 4
			if unit >= 0xdc00 && unit <= 0xdfff {
				return fail(CodeInvalidJSON, "decode", "unpaired low surrogate escape", nil)
			}
			if unit < 0xd800 || unit > 0xdbff {
				continue
			}
			if cursor+6 >= len(data) || data[cursor+1] != '\\' || data[cursor+2] != 'u' {
				return fail(CodeInvalidJSON, "decode", "unpaired high surrogate escape", nil)
			}
			low, ok := parseHexUTF16(data, cursor+3)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return fail(CodeInvalidJSON, "decode", "invalid surrogate pair", nil)
			}
			cursor += 6
		}
	}
	return nil
}

func parseHexUTF16(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value uint16
	for _, char := range data[start : start+4] {
		value <<= 4
		switch {
		case char >= '0' && char <= '9':
			value |= uint16(char - '0')
		case char >= 'a' && char <= 'f':
			value |= uint16(char-'a') + 10
		case char >= 'A' && char <= 'F':
			value |= uint16(char-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

var jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

func validateRequiredFields(value JSONValue, targetType reflect.Type) error {
	if targetType == nil {
		return nil
	}
	for targetType.Kind() == reflect.Pointer {
		if value == nil {
			return nil
		}
		targetType = targetType.Elem()
	}
	if targetType.Implements(jsonUnmarshalerType) || reflect.PointerTo(targetType).Implements(jsonUnmarshalerType) {
		return nil
	}
	switch targetType.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]JSONValue)
		if !ok {
			return fail(CodeInvalidJSON, "typed-decode", "expected object for typed struct", nil)
		}
		for index := 0; index < targetType.NumField(); index++ {
			field := targetType.Field(index)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			child, present := object[tag]
			if !present {
				return fail(CodeInvalidJSON, "typed-decode", fmt.Sprintf("required field %q is missing", tag), nil)
			}
			if err := validateRequiredFields(child, field.Type); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]JSONValue)
		if !ok {
			return fail(CodeInvalidJSON, "typed-decode", "expected array for typed slice", nil)
		}
		for _, child := range array {
			if err := validateRequiredFields(child, targetType.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
