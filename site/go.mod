// The site generator is a separate module on purpose: it needs a Markdown
// parser, and the slidepack module itself is meant to stay at exactly one
// dependency. Nothing here ships in the binary.
module github.com/pridkett/slidepack/site

go 1.24

require github.com/yuin/goldmark v1.8.5
