package crl

import "testing"

func TestRenderCountCallStopsAtItsMatchingParenthesis(t *testing.T) {
	tokens, err := Lex("count(a, b) >= count(c, d)\n")
	if err != nil {
		t.Fatal(err)
	}

	call, next, ok := renderCountCall(tokens, 0)
	if !ok {
		t.Fatal("first count call was not recognized")
	}
	if call != "count(a, b)" {
		t.Fatalf("first call swallowed later tokens: got %q", call)
	}
	if next+1 >= len(tokens) || tokens[next+1].Literal != ">=" {
		t.Fatalf("parser stopped at token %d; want the first matching parenthesis before >=", next)
	}
}
