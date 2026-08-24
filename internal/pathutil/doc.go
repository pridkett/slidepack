// Package pathutil normalizes and validates slidepack package paths.
//
// A "package path" is the canonical name a file has inside the archive and the
// manifest. It is always slash-separated, relative, clean, and UTF-8, no matter
// which operating system produced or consumes it.
//
// Every path arriving from an archive is untrusted. Validate it with Check
// before it is used to build a filesystem path, and build destination paths
// with SafeJoin, which refuses to escape its root.
package pathutil
