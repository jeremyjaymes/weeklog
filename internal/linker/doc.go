// Package linker groups events into units of work.
//
// Run is the hook weeklog collect calls after sources finish. The linking
// passes arrive in a later milestone; Run currently checks the store and
// leaves work items unchanged.
package linker
