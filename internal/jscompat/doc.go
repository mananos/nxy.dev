// Package jscompat reproduces the few JavaScript semantics the Go ports of the 1.x depend on, so
// results stay identical: truthiness and `||`, String(), UTF-16 length and slice, Date.parse of
// ISO strings and localeCompare. It is pure and every behaviour is pinned by golden files that
// Node generates (testdata/golden/jscompat).
//
// Known limits, left out on purpose: Go strings cannot hold a lone surrogate, so a UTF-16 cut that
// would split a pair stops before the pair; Date.parse falls back to V8's legacy parser for
// non-ISO text ("2026/10/31"), which is not ported (those inputs give NaN here); LocaleCompare
// is exact for ASCII only and follows the root locale (the 1.x uses the machine default);
// Date.parse of date-times without an offset uses time.Local, checked in Go only against zones
// built in the test (fixed and DST), never against V8.
package jscompat
