// Package notes reads and writes the notes table (SPEC 4.7): post a one-line
// summary with an optional Markdown body to a task, an initiative, a resource
// or the whole board, and list notes by the filters of SPEC 6.2. Each post
// appends its note_posted event in the same transaction.
package notes
