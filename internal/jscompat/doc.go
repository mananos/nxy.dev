// Package jscompat reproduces the few JavaScript semantics the Go ports of the 1.x depend on, so
// results stay identical: truthiness and `||`, String(), UTF-16 length and slice, Date.parse of
// ISO strings and localeCompare. It is pure and every behaviour is pinned by golden files that
// Node generates (testdata/golden/jscompat).
//
// Known limits, left out on purpose: Go strings cannot hold a lone surrogate, so a UTF-16 cut that
// would split a pair stops before the pair; Date.parse falls back to V8's legacy parser for
// non-ISO text ("2026/10/31"), which is not ported (those inputs give NaN here); LocaleCompare
// is exact for ASCII only and follows the root locale (the 1.x uses the machine default);
// Date.parse of date-times without an offset uses time.Local (ParseDateIn takes the zone), and
// times that repeat or do not exist at a DST change are resolved as V8 does; the golden checks five
// IANA zones through one child node each (TZ=<zone>), and only current dates: Node reads its own
// ICU rules, Windows Go the registry, so historical DST rules could differ. Further limits: ToNumber
// of arrays and objects is NaN (JS goes through String), Restringify cannot reproduce lone
// surrogates, ToFixed supports 0..100 digits, and PadStart/PadEnd cut a filler at a pair boundary.
package jscompat
