# What CRL Cannot Express

CRL v1 compares collected evidence against constants written into the
rule. This page states the boundary of that surface, so it can be
designed around rather than discovered during authoring. Everything
here is asserted by `internal/crl/expressiveness_limits_test.go` and by
the docs linter, which compiles every block below.

## The shape of a comparison

Every comparison predicate is:

```text
need <signal> <op> <literal>
```

The literal is a `number`, `bool`, or `string` constant. The compiler
parses the right-hand side as a value, not as an expression, so it
accepts no identifier and no operator of its own.

## No arithmetic

There is no arithmetic anywhere in the language. An expression on the
right of a comparison is read as one malformed literal:

```crl
# docs-lint: expect-error
crl v1
package examples.limits
bundle order.release

rule extended_value
	target order.release
	collector c org api from /order.json
		signal total number from order.total ttl 30d
		signal price number from order.unit_price ttl 30d
		signal qty number from order.quantity ttl 30d
	need total == price * qty
	quorum c
```

`crlc` reports `invalid literal "price * qty"`. The same holds for `+`,
`-`, and `/`. An expression on the left splits on its operator instead
and reports `unsupported operator "*"`.

## No signal-to-signal comparison

The sharper limit: two signals cannot be compared to each other. An
identifier on the right is read as a literal and fails to parse:

```crl
# docs-lint: expect-error
crl v1
package examples.limits
bundle order.release

rule delivered_against_ordered
	target order.release
	collector c org api from /order.json
		signal ordered number from order.ordered ttl 30d
		signal delivered number from order.delivered ttl 30d
	need delivered >= ordered
	quorum c
```

`crlc` reports `invalid literal "ordered"`.

## The one exception: temporal predicates

Temporal predicates take a reference that may name another `time`
signal, so they are the only predicates relating two signals:

```crl
crl v1
package examples.limits
bundle permit.application

rule inspection_window
	target permit.application
	collector c municipality api from /permit.json
		signal issued time from permit.issued_at ttl 10y
		signal inspected time from permit.inspected_at ttl 10y
	need inspected within 90d after issued
	need issued before inspected
	quorum c
```

`age` is not among them: `need issued age <= 30d` takes a duration
constant, and `need issued age <= inspected` is a compile error.

## `+` is not addition

`+` appears in the grammar only as subject-counting sugar inside a
quorum. `quorum a + b >= 2` renders canonically as
`quorum count(a, b) >= 2` and counts present subjects. Nothing numeric
is added.

## What this rules out

None of the following is expressible, and none has an in-language
workaround:

| Requirement | Why it does not compile |
|---|---|
| `delivered >= ordered` | signal-to-signal comparison |
| `total == quantity * unit_price` | arithmetic |
| `sum(shipments) >= order_quantity` | no aggregation |
| potency within a tolerance band of a target signal | signal-to-signal comparison |
| yield as a ratio of two measurements | arithmetic |

## The workaround, and what it costs

Compute the relation upstream and report the result as one `bool`:

```crl
crl v1
package examples.limits
bundle order.release

rule reconciled_delivery
	target order.release
	collector c org api from /order.json
		signal delivery_reconciled bool from order.reconciled ttl 30d
	need delivery_reconciled == true
	quorum c
```

State the consequence plainly: the connector now decides. The compiled
bundle and its hash certify that a boolean was true, not that delivered
met ordered. The comparison itself sits in application code that is not
compiled, not canonicalized, and not covered by the content hash. For
any rule whose substance is quantitative, this moves the governed
decision out of the governed artifact.
