package crl

import "strings"

type SyntaxTree struct {
	Tokens     []Token           `json:"tokens"`
	Statements []SyntaxStatement `json:"statements"`
}

type SyntaxStatement struct {
	Tokens []Token `json:"tokens"`
	Line   int     `json:"line"`
	Column int     `json:"column"`
	Indent int     `json:"indent"`
}

func Parse(source string) (SyntaxTree, error) {
	tokens, err := Lex(source)
	if err != nil {
		return SyntaxTree{}, err
	}
	statements, err := syntaxStatements(tokens)
	if err != nil {
		return SyntaxTree{}, err
	}
	return SyntaxTree{Tokens: tokens, Statements: statements}, nil
}

func (s SyntaxStatement) Fields() []string {
	return syntaxFields(s.Tokens)
}

func (s SyntaxStatement) OpensBlock() bool {
	for _, token := range s.Tokens {
		if token.Kind == TokenLBrace {
			return true
		}
	}
	return false
}

func (s SyntaxStatement) ClosesBlockOnly() bool {
	if len(s.Tokens) != 1 {
		return false
	}
	return s.Tokens[0].Kind == TokenRBrace
}

func syntaxStatements(tokens []Token) ([]SyntaxStatement, error) {
	var statements []SyntaxStatement
	var current []Token
	for _, token := range tokens {
		switch token.Kind {
		case TokenNewline, TokenEOF:
			if len(current) > 0 {
				statements = append(statements, newSyntaxStatement(current))
				current = nil
			}
		default:
			current = append(current, token)
		}
	}
	if len(current) > 0 {
		statements = append(statements, newSyntaxStatement(current))
	}
	return statements, nil
}

func newSyntaxStatement(tokens []Token) SyntaxStatement {
	first := tokens[0]
	return SyntaxStatement{
		Tokens: tokens,
		Line:   first.Line,
		Column: first.Column,
		Indent: first.Column - 1,
	}
}

func syntaxFields(tokens []Token) []string {
	fields := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token.Kind {
		case TokenLBrace, TokenRBrace:
			continue
		case TokenIdentifier:
			if call, next, ok := renderCountCall(tokens, i); ok {
				fields = append(fields, call)
				i = next
				continue
			}
			fields = append(fields, logicalFieldAlias(token.Literal))
		case TokenLParen:
			fields = append(fields, "(")
		case TokenRParen:
			fields = append(fields, ")")
		case TokenComma:
			fields = append(fields, ",")
		case TokenPlus:
			fields = append(fields, "+")
		default:
			fields = append(fields, token.Literal)
		}
	}
	return fields
}

func renderCountCall(tokens []Token, start int) (string, int, bool) {
	if !strings.EqualFold(tokens[start].Literal, "count") || start+1 >= len(tokens) || tokens[start+1].Kind != TokenLParen {
		return "", start, false
	}
	depth := 0
	end := -1
	for i := start + 1; i < len(tokens); i++ {
		switch tokens[i].Kind {
		case TokenLParen:
			depth++
		case TokenRParen:
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end == -1 {
		return "", start, false
	}
	var b strings.Builder
	b.WriteString(tokens[start].Literal)
	b.WriteString("(")
	for i := start + 2; i < end; i++ {
		token := tokens[i]
		switch token.Kind {
		case TokenComma:
			b.WriteString(", ")
		case TokenLParen:
			b.WriteString("(")
		case TokenRParen:
			b.WriteString(")")
		default:
			if needsCallSpace(b.String()) {
				b.WriteString(" ")
			}
			b.WriteString(logicalFieldAlias(token.Literal))
		}
	}
	b.WriteString(")")
	return b.String(), end, true
}

func needsCallSpace(current string) bool {
	return current != "" && !strings.HasSuffix(current, "(") && !strings.HasSuffix(current, " ") && !strings.HasSuffix(current, ", ")
}

func logicalFieldAlias(literal string) string {
	switch strings.ToLower(literal) {
	case "and":
		return "&"
	case "or":
		return "|"
	case "not":
		return "!"
	default:
		return literal
	}
}
