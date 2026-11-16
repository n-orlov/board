// Package idempotency reads and writes the idempotency_keys table (SPEC 4.12,
// 6.1): the answer to the first request that came with an Idempotency-Key, so
// the same request sent again within 24 hours gets that answer and writes
// nothing.
package idempotency
