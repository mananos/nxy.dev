// Package format is the pure port of core/format.mjs: number, money, duration and date text, the
// statusline gauge and ANSI styles, plain-text tables, Clip, the --since parser and the argv
// parser. Output is identical to the 1.x; every function is pinned by golden files that Node
// generates (testdata/golden/format), the date and since sets in five time zones.
//
// JS null and undefined arrive as NaN (numbers) or "" (strings). Every toFixed, Math.round,
// padStart and length goes through internal/jscompat, so widths count UTF-16 units.
//
// Edges not reproduced: ParseArgs does not model a `__proto__` key (JS would set a prototype);
// ParseSince holds the result in int64, so a relative span beyond ~9e18 ms overflows (JS keeps a
// float); a Clip or ParseSince input that JS would cut inside a surrogate pair stops before it
// (see jscompat); non-ISO strings in ParseSince give "unparseable" where V8's legacy parser reads
// them (see jscompat.ParseDate: "7", or an ISO date padded with spaces, which V8 accepts); a table cell shorter than the columns is not supported (callers
// build full rows).
package format
