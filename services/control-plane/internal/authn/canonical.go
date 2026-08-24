package authn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"unicode/utf8"
)

func domainDigest(domain string, payload []byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(payload)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

type canonicalObject struct {
	buffer bytes.Buffer
	first  bool
	valid  bool
}

type canonicalValue struct {
	bytes []byte
	valid bool
}

func newCanonicalObject() *canonicalObject {
	object := &canonicalObject{first: true, valid: true}
	object.buffer.WriteByte('{')
	return object
}

func (object *canonicalObject) member(name string, value canonicalValue) {
	encodedName := jsonString(name)
	if !object.valid || !encodedName.valid || !value.valid {
		object.valid = false
		return
	}
	if !object.first {
		object.buffer.WriteByte(',')
	}
	object.first = false
	object.buffer.Write(encodedName.bytes)
	object.buffer.WriteByte(':')
	object.buffer.Write(value.bytes)
}

func (object *canonicalObject) bytes() canonicalValue {
	if !object.valid {
		return canonicalValue{}
	}
	object.buffer.WriteByte('}')
	return canonicalValue{bytes: object.buffer.Bytes(), valid: true}
}

func jsonString(value string) canonicalValue {
	if !utf8.ValidString(value) {
		return canonicalValue{}
	}
	encoded := make([]byte, 0, len(value)+2)
	encoded = append(encoded, '"')
	for len(value) > 0 {
		character, width := utf8.DecodeRuneInString(value)
		value = value[width:]
		switch character {
		case '"', '\\':
			encoded = append(encoded, '\\', byte(character))
		case '\b':
			encoded = append(encoded, '\\', 'b')
		case '\t':
			encoded = append(encoded, '\\', 't')
		case '\n':
			encoded = append(encoded, '\\', 'n')
		case '\f':
			encoded = append(encoded, '\\', 'f')
		case '\r':
			encoded = append(encoded, '\\', 'r')
		default:
			if character < 0x20 {
				const hexadecimal = "0123456789abcdef"
				encoded = append(encoded, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0xf])
			} else {
				encoded = utf8.AppendRune(encoded, character)
			}
		}
	}
	return canonicalValue{bytes: append(encoded, '"'), valid: true}
}

func jsonStringArray(values []string) canonicalValue {
	var buffer bytes.Buffer
	buffer.WriteByte('[')
	for index, value := range values {
		encoded := jsonString(value)
		if !encoded.valid {
			return canonicalValue{}
		}
		if index > 0 {
			buffer.WriteByte(',')
		}
		buffer.Write(encoded.bytes)
	}
	buffer.WriteByte(']')
	return canonicalValue{bytes: buffer.Bytes(), valid: true}
}

func jsonInteger(value int64) canonicalValue {
	return canonicalValue{bytes: []byte(strconv.FormatInt(value, 10)), valid: true}
}

func jsonBoolean(value bool) canonicalValue {
	if value {
		return canonicalValue{bytes: []byte("true"), valid: true}
	}
	return canonicalValue{bytes: []byte("false"), valid: true}
}
