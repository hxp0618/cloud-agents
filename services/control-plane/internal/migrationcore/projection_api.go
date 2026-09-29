package migrationcore

import (
	"encoding/json"
	"strings"

	"unicode/utf8"
)

type principalClosureShapeViolation uint8

const (
	principalClosureValid principalClosureShapeViolation = iota
	principalClosureEmpty
	principalClosureLimit
	principalClosureInvalidIdentity
	principalClosureNonCanonicalOrder
)

func checkPrincipalClosureShape(principals []string) principalClosureShapeViolation {
	if len(principals) == 0 {
		return principalClosureEmpty
	}
	if uint64(len(principals)) > projectionMaxPrincipals {
		return principalClosureLimit
	}
	for index, principal := range principals {
		if principal == "" || !utf8.ValidString(principal) || strings.ContainsRune(principal, '\x00') {
			return principalClosureInvalidIdentity
		}
		if index > 0 && strings.Compare(principals[index-1], principal) >= 0 {
			return principalClosureNonCanonicalOrder
		}
	}
	return principalClosureValid
}

func cloneProjectionValue[T any](value T) T {
	raw, err := json.Marshal(value)
	if err != nil {
		var zero T
		return zero
	}
	var cloned T
	if err := json.Unmarshal(raw, &cloned); err != nil {
		var zero T
		return zero
	}
	return cloned
}
