# calc spec

`calc.evaluate(text, variables=None) -> float` evaluates an arithmetic expression.
`calc.ExprError(message, pos)` is raised on any problem (subclass of `Exception`); `.pos` is a
0-based index into `text`. It must NOT be done with `eval`, `exec`, `ast` or `compile`.

## Lexical

* Whitespace (space, tab) between tokens is ignored.
* Number: one or more digits, optionally followed by `.` and one or more digits (`3`, `3.25`).
  `.5` and `5.` are errors: the offending `.` is an unexpected character (pos of the dot).
* Identifier: a letter or `_`, then letters, digits or `_`.
* Operators and punctuation: `+ - * / % ^ ( ) ,`.
* Any other character is an error at its position.
* Results are always `float`.

## Grammar and precedence (loosest to tightest)

1. `+` `-` binary, left-associative.
2. `*` `/` `%` binary, left-associative. `%` is Python's floor modulo (`-7 % 3 == 2`).
3. unary `+` `-` (prefix, may repeat: `--3` is 3).
4. `^` power, RIGHT-associative: `2^3^2 == 512`.

Unary minus binds LOOSER than `^` on its left: `-2^2 == -4`. But the exponent itself may carry a
sign: `2^-1 == 0.5`, and `2^-2^2 == 2^(-(2^2)) == 0.0625`.

Primary: number, variable, `(` expr `)`, or function call `name(arg, arg, ...)`.

## Names

* Variables come from the `variables` dict. A name that is not in it is an error at the
  identifier's position.
* Functions: `min(a, b, ...)` and `max(a, b, ...)` take 1 or more args; `abs(x)` and `sqrt(x)` take
  exactly 1. Any other name followed by `(` is an unknown function. A wrong number of args is an
  error at the position of the function name. `sqrt` of a negative number is an error at the
  position of the function name. A variable name used with `(` is NOT a call: names followed by
  `(` are always looked up as functions.

## Errors and positions (all raise ExprError)

| situation | pos |
|---|---|
| unexpected character | that character |
| a token where an operand is expected (`1 + * 2`, `)`, `,`) | that token |
| input ends where an operand is expected (`1 +`, `""`, `"   "`, `2 * (`) | `len(text)` |
| missing `)` at the end of input | `len(text)` |
| `)` with no opening `(` | that `)` |
| leftover tokens after a complete expression (`1 2`) | the first leftover token |
| unknown variable / function | the identifier |
| wrong argument count, negative sqrt | the function name |
| division or modulo by zero | the operator (`/` or `%`) |
| float overflow (`10^1000`, or any result that is inf) | the `^` / `*` / `+` ... operator |

The message text is free-form but must be non-empty.
