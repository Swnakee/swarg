# Style sheet spec for site/style.css

One file, at least 250 lines, plain CSS (no preprocessor). Sections in this order, each with a
comment header:

1. reset: `*`, `html`, `body`
2. variables on `:root` (colors, spacing, radius, font)
3. `header`, `.logo`
4. `.nav`, `.nav a`, `.nav a:hover`
5. `.hero`, `.hero h1`, `.hero p`, `.btn`
6. `.cards`, `.card`, `.card h2`, `.card p`
7. `form`, `label`, `input`, `input:focus`, `button`
8. `footer`
9. `.dark` theme overrides for body, header, .card
10. `@media (max-width: 768px)` and `@media (max-width: 480px)`
