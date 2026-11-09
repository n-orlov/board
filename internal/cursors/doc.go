// Package cursors reads and writes the cursors table (SPEC 4.8): the position an
// actor last read on the notes stream and on the events stream, so a reader may
// ask for "since my last read" (`since=cursor`) without keeping state itself.
package cursors
