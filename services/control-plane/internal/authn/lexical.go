package authn

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var scopeItemExpression = regexp.MustCompile(`^[a-z][a-z0-9-]*\.(create|get|list|watch|update|delete|act|bind)$`)

func validExactString(value string, maximumScalars int, allowControls bool) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximumScalars {
		return false
	}
	if !allowControls {
		for _, character := range value {
			if character <= 0x1f || character == 0x7f {
				return false
			}
		}
	}
	return true
}

func validOpaqueIdentifier(value string, maximumBytes int) bool {
	if len(value) == 0 || len(value) > maximumBytes {
		return false
	}
	alphanumeric := func(character byte) bool {
		return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
	}
	if !alphanumeric(value[0]) || !alphanumeric(value[len(value)-1]) {
		return false
	}
	for index := 1; index+1 < len(value); index++ {
		character := value[index]
		if !alphanumeric(character) && character != '.' && character != '_' && character != '~' && character != '-' {
			return false
		}
	}
	return true
}

func validAbsoluteURI(value string, maximumScalars int) bool {
	if !validExactString(value, maximumScalars, false) {
		return false
	}
	colon := strings.IndexByte(value, ':')
	if colon < 1 || !asciiAlpha(value[0]) {
		return false
	}
	for index := 1; index < colon; index++ {
		character := value[index]
		if !asciiAlpha(character) && !(character >= '0' && character <= '9') && character != '+' && character != '-' && character != '.' {
			return false
		}
	}
	for index := colon + 1; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !asciiHex(value[index+1]) || !asciiHex(value[index+2]) {
			return false
		}
		index += 2
	}
	return true
}

func asciiAlpha(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func asciiHex(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'A' && character <= 'F' || character >= 'a' && character <= 'f'
}

func parseScopes(value string, maximumItems, minimumBytes, maximumBytes int) ([]string, bool) {
	if value == "" || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") || strings.Contains(value, "  ") {
		return nil, false
	}
	items := strings.Split(value, " ")
	if len(items) == 0 || len(items) > maximumItems {
		return nil, false
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if len(item) < minimumBytes || len(item) > maximumBytes || !scopeItemExpression.MatchString(item) {
			return nil, false
		}
		if _, duplicate := seen[item]; duplicate {
			return nil, false
		}
		seen[item] = struct{}{}
	}
	sort.Strings(items)
	return items, true
}

func sortedUnique(values []string, maximumItems int, validate func(string) bool) ([]string, bool) {
	if len(values) > maximumItems {
		return nil, false
	}
	copyOfValues := append([]string(nil), values...)
	sort.Strings(copyOfValues)
	for index, value := range copyOfValues {
		if !validate(value) || index > 0 && copyOfValues[index-1] == value {
			return nil, false
		}
	}
	return copyOfValues, true
}
