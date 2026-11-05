// Package audit upserts actors and appends events, the two writes every
// mutating operation makes in its own transaction (SPEC 4.1, 4.9).
package audit
