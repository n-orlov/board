// Package checkin assembles the agent check-in behind GET /api/v1/now (SPEC
// 6.5): the lease and conflict parts, the asks the actor raised, the notes that
// concern it and the attention counts. It only reads: no cursor moves.
package checkin
