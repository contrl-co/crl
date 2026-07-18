# CRL v1 Syntax

CRL source is line-oriented: newlines separate statements, and there is
no statement terminator. Two equivalent layout styles exist —
indentation form and object-block form with `{` and `}` — and both
compile to the same object model, the same canonical text, and the same
hash.

## Version header

A source file should begin with a version statement:

```text
crl v1
```

The header takes exactly one argument: `v1` or its canonical spelling
`crl/v1`. Any other value is a compile error. The header is optional —
a source without one compiles as v1 — but the linter warns (`CRL200`)
when it is missing, and hand-authored files should always carry it.

## Lexical rules

### Comments

`#` starts a comment; it runs to the end of the line and may follow
code on the same line. There is no other comment syntax — in
particular, `//` is not a comment and will not compile.

```crl
# docs-lint: expect-error
crl v1
// this is not a CRL comment
```

### Whitespace and statements

Spaces and tabs separate tokens. Newlines end statements. In
indentation form, any indented line belongs to the innermost open
declaration; indentation depth beyond that is not significant. A tab
counts as one column.

### Identifiers

Object names and fact fields are identifiers:

```text
[A-Za-z_][A-Za-z0-9_.-]*
```

Identifiers are normalized to lowercase at compile time, so names are
effectively case-insensitive; the canonical form always renders them in
lowercase. Dots are legal inside identifiers and conventionally used as
namespace separators (`permit.application`).

Statement keywords (`rule`, `need`, `signal`, …) are not reserved, but
using them as object names makes source hard to read; the style is to
avoid it.

### Strings

Strings are delimited by single or double quotes, and a backslash
escapes the next character. A string may not contain an unescaped
newline. Use strings for sources or values containing spaces or
characters outside the unquoted source alphabet
(`[A-Za-z0-9_./:@?-]`).

### Numbers

Numbers are finite decimals with an optional leading `-` and at most
one decimal point. They are carried as 64-bit floating point; NaN and
infinities are rejected.

### Booleans

`true` and `false`, matched case-insensitively.

### Durations

A duration is a positive integer (no leading zero) plus a unit:

| Unit | Meaning |
|---|---|
| `ms` | accepted, but the compiled duration coerces to exactly one second — sub-second precision is not representable (lint `CRL206`). The literal is preserved in the canonical text, so `ttl 500ms` and `ttl 1s` are semantically identical yet hash differently |
| `s` | seconds |
| `m` | minutes |
| `h` | hours |
| `d` | days |
| `w` | weeks (7 days) |
| `y` | years, as exactly **365 days** — no leap-year handling (lint `CRL207`) |

## Source forms

### Indentation form

```crl
crl v1
package examples.utility
bundle buildability.power

rule power_to_site
	target utility.power
	collector utility_record utility file_upload from /bundles/power.json
		signal power_built bool from power.built ttl 10y
		signal capacity_kw number from power.capacity_kw unit kw ttl 10y
		signal grid_hold bool from power.grid_hold ttl 30d
	need power_built == true
	need capacity_kw >= 2000
	block grid_hold
	quorum utility_record
```

### Object-block form

```crl
crl v1
package examples.utility

bundle buildability.power {
    rule power_to_site {
        target utility.power
        collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1 {
            signal power_built bool from power.built ttl 10y
            signal capacity_kw number from power.capacity_kw unit kw ttl 10y
            signal grid_hold bool from power.grid_hold ttl 30d
        }
        need power_built == true
        need capacity_kw >= 2000
        block grid_hold
        quorum utility_record
    }
}
```

An opening `{` sits at the end of the statement that opens the block.
A closing `}` must be alone on its own line (a trailing comment is
fine). Anything else on a `}` line is a compile error:

```crl
# docs-lint: expect-error
crl v1
rule bad_rule {
	target a.b
	collector c org api from /x.json {
		signal s bool from x ttl 30d
	} need s == true
}
```

## Declarations

### package and bundle

```text
package <identifier>
bundle <identifier>
```

Both are optional and both take exactly one name. `bundle <name> {`
may open a block that wraps the whole bundle body. The linter warns
when either is missing (`CRL201`, `CRL202`): a compiled bundle should
be attributable without external context.

### rule

```text
rule <name> [extends <parent>]
```

A concrete rule's body must contain:

- a `target <aspect>` — the thing the rule authorizes. Targets
  conventionally carry a namespace segment (`permit.application`, not
  `permit`); the linter warns otherwise (`CRL204`). Write exactly one:
  the compiler accepts repeated `target` lines and the last one wins,
  but relying on that is poor style;
- one or more collectors, each declaring at least one signal;
- one or more predicates (`need`, `block`, `quorum`).

A rule with no target, no collectors, a collector with no signals, or
no predicates is a compile error. Apart from signals binding to the
immediately preceding collector, body statements may appear in any
order; the canonical form always renders target, then collectors, then
predicates.

### abstract rule / constructor

```text
abstract rule <name> [extends <parent>]
constructor <name> [extends <parent>]
```

The two spellings are synonyms. An abstract rule is a reusable body of
collectors and predicates that concrete rules inherit with `extends`;
it does not itself appear in the compiled bundle, and it does not
require a `target`. `extends` may only name an abstract rule —
extending a concrete rule is a compile error, as are inheritance
cycles.

Inheritance is expansion: the parent's collectors and predicates are
prepended to the child's, and the child's `target` wins when both
declare one. See [semantics.md](semantics.md#rule-inheritance).

### collector

```text
collector <name> <provider_type> <connector_kind> from <source> [schema <schema>]
```

Exactly these positions — six fields, or eight with `schema`. The
`source` is a free-form locator (quote it if it contains spaces);
`name`, `provider_type`, `connector_kind`, and `schema` are
identifiers. What a collector *connects to* is host infrastructure;
the language only fixes the declaration shape.

### signal

```text
signal <name> <kind> from <field_path> [unit <unit>] [required|optional] (ttl <duration> | expires <duration|RFC3339>)
```

A signal binds a typed fact to the **immediately preceding collector**
— a signal before any collector is a compile error. Attributes:

- `kind` is one of `number`, `bool`, `string`, `time`;
- `unit <identifier>` is allowed only on `number` signals;
- `required` (the default) or `optional` — declarative metadata for
  connectors; evaluation is unchanged either way (a `need` on an
  optional signal still requires the fact to be present);
- an expiry is **mandatory and must be the last attribute**:
  - `ttl <duration>` — evidence is fresh for the duration after its
    observation time;
  - `expires <duration>` — same as `ttl`;
  - `expires <RFC3339>` — an absolute expiry instant.

Every signal name in a bundle maps to one fact. Re-declaring a name
with a different kind, or with a different expiry, is a compile error.

### cluster

```text
cluster <name>
	rules <rule> [+ <rule>]...
	<predicate>+
```

A cluster groups concrete rules declared anywhere in the bundle
(forward references are fine — rules always evaluate before clusters)
and adds its own predicates. `rules` lists member rules joined by `+`. A cluster with
no rules or no predicates is a compile error. In indentation form a
cluster's body must be indented (unlike rules, cluster bodies get no
top-level carve-out).

### Predicates

#### need — comparison form

```text
need <field> <op> <literal>
```

`<op>` is one of `==`, `!=`, `>`, `>=`, `<`, `<=`. Numbers support all
six; `bool` and `string` fields support only `==` and `!=`. The field
must resolve to a declared signal (or other visible subject — see
[semantics.md](semantics.md#what-a-predicate-may-reference)); the
literal's type must match the field's kind.

#### need — temporal forms

```text
need <field> before <reference>
need <field> after <reference>
need <field> within <duration> before <reference>
need <field> within <duration> after <reference>
need <field> age <= <duration>
need <field> age >= <duration>
```

`<field>` must be a `time` signal. `<reference>` is `now`, an RFC3339
timestamp, or the name of another `time` signal.

#### block

```text
block <field>
```

The field must be a `bool` or `number` signal. When the fact is `true`
(or non-zero), the blocker is active and the outcome is `BLOCKED`.

#### quorum

Three surface forms:

```text
quorum <boolean-expression>
quorum count(<subject>, <subject>...) >= <n>
quorum <n> of <m> <subject> <subject>...
```

The boolean form combines subjects with `&`/`and`, `|`/`or`,
`!`/`not`, and parentheses; the lexer also accepts `&&` and `||`.
Precedence, tightest first: `not`, `and`, `or`.

The count form requires at least `n` of the listed subjects to be
present, where `n` is a positive integer. `count(a, b) >= n` may also
be spelled `a + b >= n`; both render canonically as the `count()`
form.

`n of m` is pure sugar for the count form: `quorum 2 of 3 a b c`
compiles to exactly the same bundle — and the same hash — as
`quorum count(a, b, c) >= 2`. `m` must equal the number of listed
subjects, and `1 <= n <= m`, otherwise the compile fails:

```crl
# docs-lint: expect-error
crl v1
rule bad_quorum
	target a.b
	collector c1 org api from /x.json
		signal s1 bool from x.y ttl 30d
	collector c2 org api from /y.json
		signal s2 bool from y.z ttl 30d
	need s1 == true
	quorum 3 of 2 c1 c2
```

## Grammar

Schematic grammar of the v1 authoring surface. Newlines separate
statements; `{`/`}` blocks are interchangeable with indentation.

```text
source           = version?, package?, bundle_header?, item+
                 ; the compiler is lenient about position and repeats
                 ; of these three headers (last occurrence wins);
                 ; idiomatic source puts each once, at the top
version          = "crl", ("v1" | "crl/v1")
package          = "package", identifier
bundle_header    = "bundle", identifier

item             = constructor | abstract_rule | rule | cluster | predicate

constructor      = "constructor", identifier, extends?, abstract_body
abstract_rule    = "abstract", "rule", identifier, extends?, abstract_body
rule             = "rule", identifier, extends?, rule_body
extends          = "extends", identifier
abstract_body    = target?, collector*, predicate*
rule_body        = target, collector+, predicate+
                 ; after inheritance expansion, every concrete rule
                 ; must have >=1 collector and >=1 predicate
target           = "target", identifier

collector        = "collector", identifier, identifier, identifier,
                   "from", source_path, ("schema", identifier)?, signal+
signal           = "signal", identifier, signal_kind, "from", source_path,
                   signal_attribute*, expiry
signal_attribute = "unit", identifier | "required" | "optional"
signal_kind      = "number" | "bool" | "string" | "time"
expiry           = "ttl", duration
                 | "expires", duration
                 | "expires", rfc3339_timestamp

cluster          = "cluster", identifier, cluster_rules, predicate+
cluster_rules    = "rules", identifier, ("+", identifier)*

predicate        = need | block | quorum
need             = "need", identifier, comparison_op, literal
                 | "need", identifier, ("before" | "after"), temporal_ref
                 | "need", identifier, "within", duration,
                   ("before" | "after"), temporal_ref
                 | "need", identifier, "age", ("<=" | ">="), duration
block            = "block", identifier
quorum           = "quorum", quorum_expr
                 | "quorum", "count(", identifier, (",", identifier)*, ")",
                   ">=", positive_integer
                 | "quorum", identifier, ("+", identifier)+,
                   ">=", positive_integer
                   ; a single-subject count quorum must use count()
                 | "quorum", positive_integer, "of", positive_integer,
                   identifier+

quorum_expr      = quorum_or
quorum_or        = quorum_and, (("or" | "|"), quorum_and)*
quorum_and       = quorum_unary, (("and" | "&"), quorum_unary)*
quorum_unary     = ("not" | "!"), quorum_unary
                 | "(", quorum_expr, ")"
                 | identifier

comparison_op    = "==" | "!=" | ">" | ">=" | "<" | "<="
literal          = number | bool | string
temporal_ref     = identifier | rfc3339_timestamp | "now"
```
