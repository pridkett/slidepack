// Package runtime holds the browser-side bootstrap that is embedded into every
// packed presentation.
//
// The assets are compiled into the binary with go:embed so that the slidepack
// executable is self-contained: there is no runtime directory to install and
// nothing is ever fetched from the network, at pack time or at view time.
package runtime

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed bootstrap.js
var bootstrapJS string

//go:embed bootstrap.css
var bootstrapCSS string

// JS returns the browser runtime source.
func JS() string { return bootstrapJS }

// CSS returns the bootstrap stylesheet.
func CSS() string { return bootstrapCSS }

// Check verifies that neither asset can terminate the <script> or <style>
// element it will be embedded in.
//
// This is a build-time invariant, not a defence against hostile input: the
// assets are our own source. It exists so that adding an innocent-looking
// string literal to bootstrap.js can never silently produce HTML that breaks
// out of its element.
func Check() error {
	for name, body := range map[string]string{"bootstrap.js": bootstrapJS, "bootstrap.css": bootstrapCSS} {
		lower := strings.ToLower(body)
		for _, bad := range []string{"</script", "<!--", "</style"} {
			if strings.Contains(lower, bad) {
				return fmt.Errorf("%s contains the sequence %q, which would terminate its embedding element", name, bad)
			}
		}
	}
	return nil
}
