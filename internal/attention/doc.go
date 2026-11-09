// Package attention answers "what needs the operator now" (SPEC 5.3): the five
// sections of open asks, done-not-closed tasks, expired leases nobody has
// acknowledged, overdue watches and live resource conflicts, each newest
// relevant first and each item carrying the actors involved (handle and way
// back), plus the count of every section. The sections are derived on read from
// the asks, tasks, leases and events tables; nothing here writes except the
// lazy expiry of leases that have run out, as on every other lease read.
package attention
