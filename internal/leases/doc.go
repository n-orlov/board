// Package leases reads and writes the leases and lease_resources tables (SPEC
// 4.4, 5.1, 5.2): start, renew, end and acknowledge a lease, expire the ones
// past their time, and compute the conflicts a start or renew returns. Each write
// appends its event in the same transaction.
package leases
