// Package textdiff is the port of core/diff.mjs: a pure line diff (common prefix and suffix
// trimmed, LCS in the middle, whole replace past MaxLCSCells) and its unified-diff text. Results
// must be identical to the 1.x JS, including the tie rule of the LCS walk.
//
// In the 1.x it served the baseline/gitstate/inventory review: it diffed the copy kept before a
// plan's first edit against the file as it is. The v2 review diffs with git (platform.GitDiff,
// F2.4), so nothing calls this package: it stays with its golden until a feature needs a diff
// outside git.
//
// Differences from the 1.x: lines are Go strings, so equality is byte equality (the same as JS
// for valid UTF-16; lone surrogates cannot come out of valid UTF-8 input).
package textdiff
