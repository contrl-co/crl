package crl

import (
	"errors"
	"strings"
	"testing"
)

// Every comparison `need` is `signal OP literal`. parseValue accepts a
// quoted string, true/false, or a float64, never an identifier and never
// an expression. These tests pass today; if that boundary moves, they
// fail and force a deliberate update.

func numericRule(predicate string) string {
	return "crl v1\npackage t.limits\nbundle t.limits\n" +
		"rule r\n" +
		"\ttarget order.release\n" +
		"\tcollector c org api from /x.json\n" +
		"\t\tsignal total number from x.total ttl 30d\n" +
		"\t\tsignal price number from x.price ttl 30d\n" +
		"\t\tsignal qty number from x.qty ttl 30d\n" +
		"\tneed " + predicate + "\n" +
		"\tquorum c\n"
}

func compileErrorFor(t *testing.T, predicate string) error {
	t.Helper()
	_, err := CompileBundle(numericRule(predicate))
	if err == nil {
		t.Fatalf("need %s compiled; the expressiveness boundary has moved", predicate)
	}
	return err
}

func TestArithmeticInNeedIsNotExpressible(t *testing.T) {
	cases := []struct {
		predicate string
		sentinel  error
		message   string
	}{
		{"total == price * qty", ErrInvalidSyntax, `invalid literal "price * qty"`},
		{"total >= price + 100", ErrInvalidSyntax, `invalid literal "price + 100"`},
		{"total <= total - 5", ErrInvalidSyntax, `invalid literal "total - 5"`},
		// Split on the operator instead, so it surfaces as a bad operator.
		{"qty * 2 >= 10", ErrUnsupportedOp, `"*"`},
	}
	for _, testCase := range cases {
		t.Run(testCase.predicate, func(t *testing.T) {
			err := compileErrorFor(t, testCase.predicate)
			if !errors.Is(err, testCase.sentinel) {
				t.Fatalf("error = %v, want %v", err, testCase.sentinel)
			}
			if !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), testCase.message)
			}
		})
	}
}

// A signal against a constant is the whole numeric surface.
func TestSignalAgainstConstantCompiles(t *testing.T) {
	if _, err := CompileBundle(numericRule("total >= 250000")); err != nil {
		t.Fatalf("signal against a numeric constant must compile: %v", err)
	}
}

func TestSignalToSignalComparisonIsNotExpressible(t *testing.T) {
	for _, predicate := range []string{"total >= qty", "total == price", "price < total"} {
		t.Run(predicate, func(t *testing.T) {
			err := compileErrorFor(t, predicate)
			if !errors.Is(err, ErrInvalidSyntax) {
				t.Fatalf("error = %v, want ErrInvalidSyntax", err)
			}
			if !strings.Contains(err.Error(), "invalid literal") {
				t.Fatalf("error = %q, want it to contain \"invalid literal\"", err.Error())
			}
		})
	}
}

// The consequence for supply chain and tolerance bands: each of these
// has to be computed by a connector and fed in as a boolean, which puts
// the deciding logic outside the compiled, hashed artifact.
func TestReconciliationPredicatesAreNotExpressible(t *testing.T) {
	source := "crl v1\npackage t.limits\nbundle t.limits\n" +
		"rule r\n" +
		"\ttarget order.release\n" +
		"\tcollector c org api from /x.json\n" +
		"\t\tsignal ordered number from x.ordered ttl 30d\n" +
		"\t\tsignal delivered number from x.delivered ttl 30d\n" +
		"\t\tsignal unit_price number from x.unit_price ttl 30d\n" +
		"\tneed %s\n" +
		"\tquorum c\n"
	for _, predicate := range []string{
		"delivered >= ordered",
		"delivered <= ordered",
		"delivered * unit_price >= 100000",
	} {
		t.Run(predicate, func(t *testing.T) {
			if _, err := CompileBundle(strings.Replace(source, "%s", predicate, 1)); err == nil {
				t.Fatalf("need %s compiled; CRL has gained quantitative reconciliation", predicate)
			}
		})
	}
}

// Temporal predicates are the only ones that relate two signals. `age`
// is not among them: its right-hand side is a duration constant.
func TestTemporalPredicatesAreTheOnlySignalToSignalRelation(t *testing.T) {
	temporalRule := func(predicate string) string {
		return "crl v1\npackage t.limits\nbundle t.limits\n" +
			"rule r\n" +
			"\ttarget permit.application\n" +
			"\tcollector c org api from /x.json\n" +
			"\t\tsignal issued time from x.issued ttl 30d\n" +
			"\t\tsignal inspected time from x.inspected ttl 30d\n" +
			"\tneed " + predicate + "\n" +
			"\tquorum c\n"
	}
	for _, predicate := range []string{
		"issued before inspected",
		"issued after inspected",
		"issued within 90d before inspected",
		"issued within 90d after inspected",
	} {
		t.Run("relates/"+predicate, func(t *testing.T) {
			if _, err := CompileBundle(temporalRule(predicate)); err != nil {
				t.Fatalf("temporal predicate over two time signals must compile: %v", err)
			}
		})
	}
	t.Run("age takes a duration, not a signal", func(t *testing.T) {
		_, err := CompileBundle(temporalRule("issued age <= inspected"))
		if err == nil {
			t.Fatal("age has gained a signal reference")
		}
		if !strings.Contains(err.Error(), "invalid duration") {
			t.Fatalf("error = %q, want it to contain \"invalid duration\"", err.Error())
		}
	})
}

// `+` in the grammar is subject-counting sugar, not addition.
func TestPlusInQuorumIsSubjectCountingNotAddition(t *testing.T) {
	source := "crl v1\npackage t.limits\nbundle t.limits\n" +
		"rule r\n" +
		"\ttarget order.release\n" +
		"\tcollector ca org api from /a.json\n" +
		"\t\tsignal a bool from x.a ttl 30d\n" +
		"\tcollector cb org api from /b.json\n" +
		"\t\tsignal b bool from x.b ttl 30d\n" +
		"\tquorum ca + cb >= 2\n"
	compiled, err := CompileBundle(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(compiled.CanonicalText, "count(ca, cb) >= 2") {
		t.Fatalf("canonical form = %q, want the + form rendered as count()", compiled.CanonicalText)
	}
}
