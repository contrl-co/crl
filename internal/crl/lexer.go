package crl

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSourceBytes bounds the size of a single CRL source. The lexer
// materializes the whole source as a rune slice up front, so memory is
// linear in input length before any token-level limit can apply; this
// cap is the one that actually bounds that allocation. It is far above
// any hand-authored rule (the entire example corpus is a few KB) and
// exists only to stop a pathological multi-hundred-megabyte input.
const maxSourceBytes = 8 << 20 // 8 MiB

type TokenKind string

const (
	TokenEOF        TokenKind = "eof"
	TokenNewline    TokenKind = "newline"
	TokenIdentifier TokenKind = "identifier"
	TokenString     TokenKind = "string"
	TokenNumber     TokenKind = "number"
	TokenBool       TokenKind = "bool"
	TokenOperator   TokenKind = "operator"
	TokenLBrace     TokenKind = "lbrace"
	TokenRBrace     TokenKind = "rbrace"
	TokenLParen     TokenKind = "lparen"
	TokenRParen     TokenKind = "rparen"
	TokenComma      TokenKind = "comma"
	TokenPlus       TokenKind = "plus"
)

type Token struct {
	Kind    TokenKind `json:"kind"`
	Literal string    `json:"literal"`
	Line    int       `json:"line"`
	Column  int       `json:"column"`
}

// maxTokens caps how many tokens the lexer will materialize. The lexer emits
// one token per delimiter/operator, so a pathological source (e.g. millions of
// nested parens in a quorum expression) would otherwise build a multi-million
// entry slice — gigabytes of transient memory — before any parser-level size
// check runs. No hand-authored bundle approaches this limit; past it we fail
// closed. Rejection-only: it never changes how a valid source tokenizes.
const maxTokens = 1_000_000

func Lex(source string) ([]Token, error) {
	if len(source) > maxSourceBytes {
		return nil, fmt.Errorf("%w: source is too large (%d bytes, limit %d)", ErrInvalidSyntax, len(source), maxSourceBytes)
	}
	// Validate UTF-8 before the []rune conversion below. Converting an
	// invalid byte to a rune silently substitutes U+FFFD, which would
	// erase the original bytes before they could be rejected — two
	// byte-distinct sources could then compile to one hash while the
	// evaluator still saw their raw, differing bytes. Reject up front so
	// a program's bytes are fixed before anything reads them.
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("%w: source is not valid UTF-8", ErrInvalidSyntax)
	}
	lexer := crlLexer{
		input:  []rune(source),
		line:   1,
		column: 1,
	}
	return lexer.lex()
}

type crlLexer struct {
	input  []rune
	pos    int
	line   int
	column int
}

func (l *crlLexer) lex() ([]Token, error) {
	var tokens []Token
	for !l.done() {
		if len(tokens) >= maxTokens {
			return nil, fmt.Errorf("%w: source has too many tokens (limit %d)", ErrInvalidSyntax, maxTokens)
		}
		ch := l.peek()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r':
			l.advance()
		case ch == '\n':
			tokens = append(tokens, l.token(TokenNewline, "\n"))
			l.advance()
		case ch == '#':
			l.skipComment()
		case ch == '"' || ch == '\'':
			token, err := l.lexString()
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
		case ch == '{':
			tokens = append(tokens, l.token(TokenLBrace, "{"))
			l.advance()
		case ch == '}':
			tokens = append(tokens, l.token(TokenRBrace, "}"))
			l.advance()
		case ch == '(':
			tokens = append(tokens, l.token(TokenLParen, "("))
			l.advance()
		case ch == ')':
			tokens = append(tokens, l.token(TokenRParen, ")"))
			l.advance()
		case ch == ',':
			tokens = append(tokens, l.token(TokenComma, ","))
			l.advance()
		case ch == '+':
			tokens = append(tokens, l.token(TokenPlus, "+"))
			l.advance()
		case isOperatorRune(ch):
			token, err := l.lexOperator()
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
		default:
			token, err := l.lexAtom()
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
		}
	}
	tokens = append(tokens, l.token(TokenEOF, ""))
	return tokens, nil
}

func (l *crlLexer) lexString() (Token, error) {
	startLine, startColumn := l.line, l.column
	quote := l.peek()
	var literal strings.Builder
	literal.WriteRune(quote)
	l.advance()
	escaped := false
	for !l.done() {
		ch := l.peek()
		if ch == '\n' && !escaped {
			return Token{}, fmt.Errorf("%w at line %d: unterminated string literal", ErrInvalidSyntax, startLine)
		}
		literal.WriteRune(ch)
		l.advance()
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == quote {
			return Token{Kind: TokenString, Literal: literal.String(), Line: startLine, Column: startColumn}, nil
		}
	}
	return Token{}, fmt.Errorf("%w at line %d: unterminated string literal", ErrInvalidSyntax, startLine)
}

func (l *crlLexer) lexOperator() (Token, error) {
	startLine, startColumn := l.line, l.column
	ch := l.peek()
	next := l.peekNext()
	switch {
	case ch == '=' && next == '=':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: OperatorEQ, Line: startLine, Column: startColumn}, nil
	case ch == '!' && next == '=':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: OperatorNE, Line: startLine, Column: startColumn}, nil
	case ch == '>' && next == '=':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: OperatorGTE, Line: startLine, Column: startColumn}, nil
	case ch == '<' && next == '=':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: OperatorLTE, Line: startLine, Column: startColumn}, nil
	case ch == '&' && next == '&':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: "&", Line: startLine, Column: startColumn}, nil
	case ch == '|' && next == '|':
		l.advance()
		l.advance()
		return Token{Kind: TokenOperator, Literal: "|", Line: startLine, Column: startColumn}, nil
	case ch == '!', ch == '>', ch == '<', ch == '&', ch == '|':
		l.advance()
		return Token{Kind: TokenOperator, Literal: string(ch), Line: startLine, Column: startColumn}, nil
	case ch == '=':
		l.advance()
		return Token{Kind: TokenOperator, Literal: string(ch), Line: startLine, Column: startColumn}, nil
	default:
		return Token{}, fmt.Errorf("%w at line %d: invalid operator %q", ErrInvalidSyntax, startLine, string(ch))
	}
}

func (l *crlLexer) lexAtom() (Token, error) {
	startLine, startColumn := l.line, l.column
	var literal strings.Builder
	for !l.done() {
		ch := l.peek()
		if unicode.IsSpace(ch) || ch == '#' || isDelimiterRune(ch) || isOperatorRune(ch) {
			break
		}
		literal.WriteRune(ch)
		l.advance()
	}
	if literal.Len() == 0 {
		return Token{}, fmt.Errorf("%w at line %d: unexpected character %q", ErrInvalidSyntax, startLine, string(l.peek()))
	}
	raw := literal.String()
	return Token{Kind: atomTokenKind(raw), Literal: raw, Line: startLine, Column: startColumn}, nil
}

func (l *crlLexer) skipComment() {
	for !l.done() && l.peek() != '\n' {
		l.advance()
	}
}

func (l *crlLexer) token(kind TokenKind, literal string) Token {
	return Token{Kind: kind, Literal: literal, Line: l.line, Column: l.column}
}

func (l *crlLexer) done() bool {
	return l.pos >= len(l.input)
}

func (l *crlLexer) peek() rune {
	if l.done() {
		return 0
	}
	return l.input[l.pos]
}

func (l *crlLexer) peekNext() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return l.input[l.pos+1]
}

func (l *crlLexer) advance() {
	if l.done() {
		return
	}
	ch := l.input[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.column = 1
		return
	}
	l.column++
}

func atomTokenKind(raw string) TokenKind {
	switch strings.ToLower(raw) {
	case "true", "false":
		return TokenBool
	}
	if looksNumeric(raw) {
		return TokenNumber
	}
	return TokenIdentifier
}

func looksNumeric(raw string) bool {
	if raw == "" {
		return false
	}
	seenDigit := false
	seenDot := false
	for i, ch := range raw {
		switch {
		case ch >= '0' && ch <= '9':
			seenDigit = true
		case ch == '.' && !seenDot:
			seenDot = true
		case (ch == '-' || ch == '+') && i == 0:
		default:
			return false
		}
	}
	return seenDigit
}

func isOperatorRune(ch rune) bool {
	switch ch {
	case '=', '!', '>', '<', '&', '|':
		return true
	default:
		return false
	}
}

func isDelimiterRune(ch rune) bool {
	switch ch {
	case '{', '}', '(', ')', ',':
		return true
	default:
		// '+' is NOT a mid-token delimiter: it must stay part of an RFC3339
		// offset like 2026-12-31T23:59:59+05:30 (mirroring '-', which already
		// is not a delimiter). A cluster-rule '+' is always space-separated,
		// so the main lexer loop still tokenizes it on its own.
		return false
	}
}
