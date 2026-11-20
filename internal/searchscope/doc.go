// Package searchscope is the port of core/search-scope.mjs: it classifies a search-like tool call
// (Grep, Glob, Read, or a console rg / git grep / grep -r / find / fd / Get-ChildItem /
// Select-String) as broad or targeted. Pure string work; the only outside knowledge, "is this path
// a file?", comes from the injected isFile callback, which never sees a wildcard path.
//
// Used by the metrics marks (LocateNudgeMark, IsLocateCommand) and by the broad-search scan of a
// transcript. The gate hooks of the 1.x (pretooluse-search.mjs, search-observe.mjs: nudge and read
// gate) are not ported: they wait for debate 14.3.
//
// Target and Pattern keep the raw input values, as JS does (a numeric pattern stays a number).
//
// Differences from the 1.x: strings.ToLower and (?i) fold some non-ASCII letters JS does not
// (Kelvin sign, long s), so they differ from JS only outside ASCII; the golden corpus is ASCII
// there.
package searchscope
