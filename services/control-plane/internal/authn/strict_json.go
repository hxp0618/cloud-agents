package authn

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

func strictJSONObject(raw []byte, maximumBytes, maximumDepth int) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > maximumBytes || !utf8.Valid(raw) || !validJSONUnicodeEscapes(raw) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if !strictJSONValue(decoder, 0, maximumDepth, true) {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func strictJSONValue(decoder *json.Decoder, depth, maximumDepth int, requireObject bool) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, compound := token.(json.Delim)
	if requireObject && (!compound || delimiter != '{') {
		return false
	}
	if !compound {
		return true
	}
	depth++
	if depth > maximumDepth {
		return false
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			member, err := decoder.Token()
			name, ok := member.(string)
			if err != nil || !ok {
				return false
			}
			if _, duplicate := seen[name]; duplicate {
				return false
			}
			seen[name] = struct{}{}
			if !strictJSONValue(decoder, depth, maximumDepth, false) {
				return false
			}
		}
		closing, err := decoder.Token()
		return err == nil && closing == json.Delim('}')
	case '[':
		for decoder.More() {
			if !strictJSONValue(decoder, depth, maximumDepth, false) {
				return false
			}
		}
		closing, err := decoder.Token()
		return err == nil && closing == json.Delim(']')
	default:
		return false
	}
}

// encoding/json deliberately replaces unpaired escaped surrogates. The
// verifier rejects them before decoding so decoded identity strings remain
// Unicode scalar sequences rather than implementation replacement values.
func validJSONUnicodeEscapes(raw []byte) bool {
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' {
			continue
		}
		index++
		for index < len(raw) && raw[index] != '"' {
			if raw[index] < 0x20 {
				return false
			}
			if raw[index] != '\\' {
				index++
				continue
			}
			index++
			if index >= len(raw) {
				return false
			}
			if raw[index] != 'u' {
				if !bytes.ContainsRune([]byte(`"\\/bfnrt`), rune(raw[index])) {
					return false
				}
				index++
				continue
			}
			first, ok := decodeHexQuad(raw, index+1)
			if !ok {
				return false
			}
			index += 5
			if first >= 0xdc00 && first <= 0xdfff {
				return false
			}
			if first >= 0xd800 && first <= 0xdbff {
				if index+5 >= len(raw) || raw[index] != '\\' || raw[index+1] != 'u' {
					return false
				}
				second, valid := decodeHexQuad(raw, index+2)
				if !valid || second < 0xdc00 || second > 0xdfff {
					return false
				}
				index += 6
			}
		}
		if index >= len(raw) {
			return false
		}
	}
	return true
}

func decodeHexQuad(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(raw[start:start+4]), 16, 16)
	return uint16(value), err == nil
}

func decodeCanonicalBase64url(segment string, maximumDecodedBytes int) ([]byte, bool) {
	if segment == "" || len(segment) > (maximumDecodedBytes*4+2)/3 {
		return nil, false
	}
	for index := range len(segment) {
		character := segment[index]
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_') {
			return nil, false
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil || len(decoded) > maximumDecodedBytes || base64.RawURLEncoding.EncodeToString(decoded) != segment {
		return nil, false
	}
	return decoded, true
}

func exactJSONString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func exactJSONInteger(raw json.RawMessage, minimum, maximum int64) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	start := 0
	if raw[0] == '-' {
		start = 1
	}
	if start == len(raw) || string(raw) == "-0" || len(raw)-start > 1 && raw[start] == '0' {
		return 0, false
	}
	for _, character := range raw[start:] {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	return value, err == nil && value >= minimum && value <= maximum
}
