package migration

import (
	"bytes"
	"fmt"
	"strings"
)

type SQLStatement struct {
	Index  int
	Start  int
	End    int // exclusive
	Raw    []byte
	SHA256 Digest
	Tokens []SQLToken
}

type SQLTokenKind uint8

const (
	SQLWord SQLTokenKind = iota + 1
	SQLQuotedIdentifier
	SQLLiteral
	SQLNumber
	SQLSymbol
)

type SQLToken struct {
	Kind SQLTokenKind
	Text string
}

// SplitPostgreSQLStatements preserves exact byte slices and only recognizes
// enough PostgreSQL lexical structure to find top-level terminating semicolons.
func SplitPostgreSQLStatements(sql []byte) ([]SQLStatement, error) {
	if len(sql) == 0 {
		return nil, fail(CodeInvalidSQL, "split", "SQL artifact is empty", nil)
	}
	statements := make([]SQLStatement, 0)
	start := 0
	for cursor := 0; cursor < len(sql); {
		switch sql[cursor] {
		case '-':
			if cursor+1 < len(sql) && sql[cursor+1] == '-' {
				cursor = scanLineComment(sql, cursor+2)
				continue
			}
		case '/':
			if cursor+1 < len(sql) && sql[cursor+1] == '*' {
				end, err := scanBlockComment(sql, cursor)
				if err != nil {
					return nil, err
				}
				cursor = end
				continue
			}
		case '\'':
			end, err := scanSingleQuote(sql, cursor, isEscapeStringPrefix(sql, cursor))
			if err != nil {
				return nil, err
			}
			cursor = end
			continue
		case '"':
			end, err := scanDoubleQuote(sql, cursor)
			if err != nil {
				return nil, err
			}
			cursor = end
			continue
		case '$':
			if delimiter, ok := dollarDelimiter(sql[cursor:]); ok {
				end := bytes.Index(sql[cursor+len(delimiter):], delimiter)
				if end < 0 {
					return nil, fail(CodeInvalidSQL, "split", "unterminated dollar-quoted string", nil)
				}
				cursor += len(delimiter) + end + len(delimiter)
				continue
			}
		case ';':
			raw := bytes.Clone(sql[start : cursor+1])
			tokens, err := tokenizeStatement(raw)
			if err != nil {
				return nil, err
			}
			if len(tokens) == 0 {
				return nil, fail(CodeInvalidSQL, "split", "empty statement is forbidden", nil)
			}
			statements = append(statements, SQLStatement{Index: len(statements), Start: start, End: cursor + 1, Raw: raw, SHA256: DigestBytes(raw), Tokens: tokens})
			start = cursor + 1
		}
		cursor++
	}
	if !onlyWhitespaceAndComments(sql[start:]) {
		return nil, fail(CodeInvalidSQL, "split", "final SQL statement lacks a terminating semicolon", nil)
	}
	if len(statements) == 0 {
		return nil, fail(CodeInvalidSQL, "split", "SQL artifact has no statements", nil)
	}
	return statements, nil
}

func tokenizeStatement(raw []byte) ([]SQLToken, error) {
	tokens := make([]SQLToken, 0)
	for cursor := 0; cursor < len(raw); {
		if isSQLSpace(raw[cursor]) {
			cursor++
			continue
		}
		if cursor+1 < len(raw) && raw[cursor] == '-' && raw[cursor+1] == '-' {
			cursor = scanLineComment(raw, cursor+2)
			continue
		}
		if cursor+1 < len(raw) && raw[cursor] == '/' && raw[cursor+1] == '*' {
			end, err := scanBlockComment(raw, cursor)
			if err != nil {
				return nil, err
			}
			cursor = end
			continue
		}
		if cursor+1 < len(raw) && (raw[cursor] == 'e' || raw[cursor] == 'E') && raw[cursor+1] == '\'' && (cursor == 0 || !isIdentContinue(raw[cursor-1])) {
			end, err := scanSingleQuote(raw, cursor+1, true)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, SQLToken{Kind: SQLLiteral, Text: "$STRING$"})
			cursor = end
			continue
		}
		if cursor+2 < len(raw) && (raw[cursor] == 'u' || raw[cursor] == 'U') && raw[cursor+1] == '&' && raw[cursor+2] == '\'' && (cursor == 0 || !isIdentContinue(raw[cursor-1])) {
			end, err := scanSingleQuote(raw, cursor+2, false)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, SQLToken{Kind: SQLLiteral, Text: "$STRING$"})
			cursor = end
			continue
		}
		switch raw[cursor] {
		case '\'':
			end, err := scanSingleQuote(raw, cursor, isEscapeStringPrefix(raw, cursor))
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, SQLToken{Kind: SQLLiteral, Text: "$STRING$"})
			cursor = end
			continue
		case '"':
			end, err := scanDoubleQuote(raw, cursor)
			if err != nil {
				return nil, err
			}
			quoted := strings.ReplaceAll(string(raw[cursor+1:end-1]), `""`, `"`)
			var encoded bytes.Buffer
			writeCanonicalString(&encoded, quoted)
			tokens = append(tokens, SQLToken{Kind: SQLQuotedIdentifier, Text: "@quoted:" + encoded.String()})
			cursor = end
			continue
		case '$':
			if delimiter, ok := dollarDelimiter(raw[cursor:]); ok {
				relative := bytes.Index(raw[cursor+len(delimiter):], delimiter)
				if relative < 0 {
					return nil, fail(CodeInvalidSQL, "tokenize", "unterminated dollar quote", nil)
				}
				cursor += len(delimiter) + relative + len(delimiter)
				tokens = append(tokens, SQLToken{Kind: SQLLiteral, Text: "$BODY$"})
				continue
			}
		}
		if isIdentStart(raw[cursor]) {
			end := cursor + 1
			for end < len(raw) && isIdentContinue(raw[end]) {
				end++
			}
			tokens = append(tokens, SQLToken{Kind: SQLWord, Text: strings.ToUpper(string(raw[cursor:end]))})
			cursor = end
			continue
		}
		if raw[cursor] >= '0' && raw[cursor] <= '9' {
			end := cursor + 1
			for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
				end++
			}
			tokens = append(tokens, SQLToken{Kind: SQLNumber, Text: string(raw[cursor:end])})
			cursor = end
			continue
		}
		if strings.ContainsRune("(),.;", rune(raw[cursor])) {
			tokens = append(tokens, SQLToken{Kind: SQLSymbol, Text: string(raw[cursor])})
		}
		cursor++
	}
	return tokens, nil
}

func scanLineComment(sql []byte, cursor int) int {
	for cursor < len(sql) && sql[cursor] != '\n' && sql[cursor] != '\r' {
		cursor++
	}
	return cursor
}

func scanBlockComment(sql []byte, cursor int) (int, error) {
	depth := 0
	for cursor < len(sql)-1 {
		if sql[cursor] == '/' && sql[cursor+1] == '*' {
			depth++
			cursor += 2
			continue
		}
		if sql[cursor] == '*' && sql[cursor+1] == '/' {
			depth--
			cursor += 2
			if depth == 0 {
				return cursor, nil
			}
			continue
		}
		cursor++
	}
	return 0, fail(CodeInvalidSQL, "split", "unterminated block comment", nil)
}

func scanSingleQuote(sql []byte, cursor int, escape bool) (int, error) {
	for cursor = cursor + 1; cursor < len(sql); cursor++ {
		if escape && sql[cursor] == '\\' {
			cursor++
			continue
		}
		if sql[cursor] != '\'' {
			continue
		}
		if cursor+1 < len(sql) && sql[cursor+1] == '\'' {
			cursor++
			continue
		}
		return cursor + 1, nil
	}
	return 0, fail(CodeInvalidSQL, "split", "unterminated string literal", nil)
}

func scanDoubleQuote(sql []byte, cursor int) (int, error) {
	for cursor = cursor + 1; cursor < len(sql); cursor++ {
		if sql[cursor] != '"' {
			continue
		}
		if cursor+1 < len(sql) && sql[cursor+1] == '"' {
			cursor++
			continue
		}
		return cursor + 1, nil
	}
	return 0, fail(CodeInvalidSQL, "split", "unterminated quoted identifier", nil)
}

func dollarDelimiter(sql []byte) ([]byte, bool) {
	if len(sql) < 2 || sql[0] != '$' {
		return nil, false
	}
	for index := 1; index < len(sql); index++ {
		if sql[index] == '$' {
			return sql[:index+1], true
		}
		if (index == 1 && !isIdentStart(sql[index])) || (index > 1 && !isIdentContinue(sql[index])) {
			return nil, false
		}
	}
	return nil, false
}

func isEscapeStringPrefix(sql []byte, quote int) bool {
	if quote >= 1 && (sql[quote-1] == 'e' || sql[quote-1] == 'E') && (quote == 1 || !isIdentContinue(sql[quote-2])) {
		return true
	}
	// U& strings use backslash for Unicode code points, not for quoting the
	// terminating apostrophe. Apostrophes still use SQL's doubled-quote rule.
	return false
}

func onlyWhitespaceAndComments(value []byte) bool {
	for cursor := 0; cursor < len(value); {
		if isSQLSpace(value[cursor]) {
			cursor++
			continue
		}
		if cursor+1 < len(value) && value[cursor] == '-' && value[cursor+1] == '-' {
			cursor = scanLineComment(value, cursor+2)
			continue
		}
		if cursor+1 < len(value) && value[cursor] == '/' && value[cursor+1] == '*' {
			end, err := scanBlockComment(value, cursor)
			if err != nil {
				return false
			}
			cursor = end
			continue
		}
		return false
	}
	return true
}

func isSQLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\f'
}
func isIdentStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= 0x80
}
func isIdentContinue(value byte) bool {
	return isIdentStart(value) || value >= '0' && value <= '9' || value == '$'
}

func tokenWords(tokens []SQLToken) []string {
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == SQLWord {
			words = append(words, token.Text)
		}
	}
	return words
}

func statementSummary(statement SQLStatement) string {
	words := tokenWords(statement.Tokens)
	if len(words) > 4 {
		words = words[:4]
	}
	return fmt.Sprintf("statement %d (%s)", statement.Index, strings.Join(words, " "))
}
