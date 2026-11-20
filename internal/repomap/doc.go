// Package repomap is the pure part of the port of core/index/languages.mjs and
// core/index/repomap.mjs: symbol extraction by regex for six languages, the minimal .gitignore
// matcher, building the map text from a list of files (reusing unchanged entries), the meta file
// encoding and the query. Results must be identical to the 1.x JS; node generates the golden
// (testdata/golden/repomap). Walking the filesystem and the files themselves live in
// internal/platform; here everything comes in as values.
//
// Its consumer is `nxy locate` (F3.4), through internal/platform.
//
// Differences from the 1.x, left out on purpose:
//   - Query compiles a /regex/ with RE2, so a pattern with JS-only syntax (lookahead,
//     backreferences, \s in a user pattern and other JS classes) does not compile and falls back to
//     substring, as a broken JS regex does; `.` excludes only \n there, and the g, u and y flags are
//     ignored (y would anchor the match in JS). A flag written twice is invalid in JS.
//   - Wanted difference: the 1.x always appended its own i, so `/x/i` became "ii" and fell back to
//     substring (a bug). Here an explicit i is not doubled and `/x/i` is a regex; the golden skips
//     those cases and TestExplicitIFlagIsRegex covers them.
//   - ToLower (query and path filters) folds some non-ASCII letters differently from JS
//     toLowerCase (final sigma, dotted I); the golden covers ASCII and a few accents.
//   - The rule tables use jscompat.SpaceClass for \s; everything else in them means the same in RE2.
//   - EncodeMeta writes the files in path order; the 1.x wrote them in directory order, so compare
//     meta as a value.
//   - A meta entry whose lines are not all strings is dropped (the 1.x only checks for an array).
//   - Ext follows path.extname with "/" as the only separator.
package repomap
