// Package asks reads and writes the asks table (SPEC 4.5): raise a question on
// a task, an initiative or the whole board, read and list asks, and answer or
// withdraw an open one. Each write appends its event in the same transaction.
package asks
