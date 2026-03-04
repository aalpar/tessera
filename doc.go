// Package tessera demonstrates delta-state CRDT coordination for
// distributed dedup backup metadata. Multiple workers back up files
// while concurrent GC deletes unreferenced blocks. Add-wins semantics
// ensure a block reference can never be silently dropped by concurrent
// deletion.
package tessera
