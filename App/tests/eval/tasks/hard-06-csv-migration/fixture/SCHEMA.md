# Customer migration

Command: `python3 migrate.py <input.csv> <customers.json> <rejects.json>`
At the end it prints exactly one line to stdout: `accepted <N> rejected <M>`. Exit code 0.

## Input

UTF-8 CSV (a BOM may be present), header row `id,name,email,phone,country,signup,balance,tags`
(the header is skipped, columns are positional), standard CSV quoting. Completely blank lines are
ignored. A row is numbered by its position among the non-blank DATA rows, starting at 1.

## Output

`customers.json`: a JSON list of accepted records sorted by `id`, each with the keys
`id` (int), `name` (str), `email` (str), `phone` (str or null), `country` (str), `signup` (str),
`balance_cents` (int), `tags` (list of str).

`rejects.json`: a JSON list, in input order, of `{"row": <number>, "id": <the raw first field
exactly as in the file, or "" if the row has no fields>, "reason": <code>}`.

Each rejected row has exactly one reason: the FIRST rule below that fails, checked in this order.

## Rules (in checking order)

1. `bad_columns`: the row does not have exactly 8 fields.
2. `bad_id`: after trimming spaces, `id` must be digits only and greater than 0 (int).
3. `duplicate_id`: an earlier ACCEPTED row has the same id. (A rejected row never "uses up" an id.)
4. `missing_name`: `name` is empty after collapsing every run of whitespace to one space and
   trimming. The cleaned name is the collapsed text; the case is not changed.
5. `bad_email`: after trimming and lowercasing, the email must match
   `^[^@\s]+@[^@\s]+\.[^@\s.]+$`. The cleaned email is the lowercased text.
6. `duplicate_email`: an earlier ACCEPTED row has the same cleaned email.
7. `bad_phone`: an empty phone (after trimming) is fine and gives `null`. Otherwise the text may
   contain only digits, spaces, `(`, `)`, `.`, `-` and a `+` that is the very first character;
   anything else is bad. Take the digits, then:
   * text starts with `+` -> `+` + digits;
   * digits start with `00` -> `+` + digits without the `00`;
   * exactly 10 digits -> `+1` + digits;
   * exactly 11 digits starting with `1` -> `+` + digits;
   * any other case -> bad.
   The result must be `+` followed by 8 to 15 digits, otherwise bad.
8. `bad_country`: trimmed and upper-cased, aliases are mapped (`USA`, `UNITED STATES`, `U.S.` -> `US`;
   `UK`, `UNITED KINGDOM`, `GREAT BRITAIN` -> `GB`; `GERMANY`, `DEUTSCHLAND` -> `DE`). The result must be
   one of `US GB DE FR PL UA CA`.
9. `bad_date`: after trimming, one of exactly these formats, always with 4-digit year and
   2-digit month/day: `YYYY-MM-DD`, `DD.MM.YYYY`, `MM/DD/YYYY` (month first), `YYYY/MM/DD`.
   It must be a real calendar date (leap years count) and the year must be from 1990 to 2025
   inclusive. Output format `YYYY-MM-DD`.
10. `bad_balance`: empty (after trimming) means 0. Otherwise remove all spaces and all `$` and
    `€` characters. Surrounding parentheses mean negative; a leading `-` means negative (both together
    is still just negative). What is left must be digits, optionally with comma thousands groups
    (`1,234,567` - a group is exactly 3 digits and only the first may be shorter), optionally followed by
    `.` and at least one digit. Comma is never a decimal separator. Round to cents half away from zero
    (`12.345` -> 1235, `-0.005` -> -1). `balance_cents` is the integer number of cents.

Tags (cannot fail): split on `;` and `|`, trim, lowercase, drop empty ones, remove duplicates, sort
alphabetically.
