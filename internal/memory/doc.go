// Package memory holds the rules of nxy's memory (roadmap 14.3): the rows
// (rules, closed features, learnings, notes), their validation, the text
// helpers and the versioned JSON export with its merge. It is pure and
// host-agnostic: the export JSON is the contract between machines and hosts,
// so nothing here knows about Claude Code, SQLite or the file system.
package memory
