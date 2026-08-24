package main

import (
	"flag"
	"strings"
)

// permute reorders args so that options may appear after positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, which would
// make the documented form
//
//	slidepack pack ./deck -o deck.html
//
// fail with "pack takes exactly one source directory". Moving options ahead of
// positionals before parsing gives the GNU-style behaviour people expect,
// without adding a CLI dependency.
//
// Unknown options are passed through untouched so that flag reports them.
// A bare "--" ends option processing, as usual.
func permute(fs *flag.FlagSet, args []string) []string {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if strings.ContainsRune(name, '=') {
			options = append(options, a)
			continue
		}
		f := fs.Lookup(name)
		if f != nil && !isBoolFlag(f) && i+1 < len(args) {
			options = append(options, a, args[i+1])
			i++
			continue
		}
		options = append(options, a)
	}
	return append(options, positional...)
}

// isBoolFlag reports whether a flag may appear without a following value.
func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
