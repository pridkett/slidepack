// Command site builds the slidepack documentation site.
//
// It is deliberately small: Markdown in, static HTML out, one stylesheet, no
// JavaScript, no framework. The pages that are also repository documentation
// (the source-format and packed-format specifications) are rendered from the
// very same Markdown files the repository ships, so the site cannot describe a
// different format from the one in the tree.
//
// Alongside the HTML it writes three machine-readable companions:
//
//	llms.txt        an index for language models, per the llms.txt convention
//	llms-full.txt   every page concatenated as plain text
//	cli.json        `slidepack help --json`, the complete interface description
//
// Usage:
//
//	go run . -bin /path/to/slidepack -out dist
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// BaseURL is where the site is published. It appears in canonical links and in
// llms.txt, which the convention says should carry absolute URLs.
const BaseURL = "https://pridkett.github.io/slidepack"

// FormatVersion is the packed format this documentation describes.
const FormatVersion = 1

// page is one rendered document.
type page struct {
	// Src is the Markdown source, relative to the site directory.
	Src string
	// Out is the file name to write.
	Out string
	// Title is the <title>, and the label used in llms.txt.
	Title string
	// Nav is the label in the header, or empty to leave the page out of it.
	Nav string
	// Description is the meta description and the llms.txt annotation.
	Description string
	// NumberedByHand suppresses the CSS section counter for documents that
	// number their own clauses, such as the format specification.
	NumberedByHand bool
}

func pages() []page {
	return []page{
		{
			Src: "pages/index.md", Out: "index.html",
			Title: "slidepack — reversible self-contained HTML presentations",
			Nav:   "Overview",
			Description: "Edit a directory. Pack it into one .html file that opens from the " +
				"filesystem with no server, no extension and no network — and unpack it again.",
		},
		{
			Src: "../docs/source-format.md", Out: "source-format.html",
			Title: "Source format — slidepack",
			Nav:   "Authoring",
			Description: "What a slidepack presentation directory may contain: supported " +
				"resources, how references resolve, and which constructs format v1 refuses.",
			NumberedByHand: true,
		},
		{
			Src: "../docs/format-v1.md", Out: "format-v1.html",
			Title: "Packed format v1 — slidepack",
			Nav:   "Packed format",
			Description: "The slidepack packed file format, specified completely enough to " +
				"write an independent unpacker.",
			NumberedByHand: true,
		},
		{
			Src: "pages/agents.md", Out: "agents.html",
			Title: "For agents — slidepack",
			Nav:   "Agents",
			Description: "How a program should drive slidepack: the JSON interface, the " +
				"diagnostic catalogue, and the authoring loop.",
		},
	}
}

type navItem struct {
	Href    string
	Label   string
	Current bool
}

type view struct {
	Title          string
	Description    string
	Out            string
	BaseURL        string
	FormatVersion  int
	NumberedByHand bool
	Nav            []navItem
	Content        template.HTML
}

func main() {
	bin := flag.String("bin", "", "path to a built slidepack binary, for generating cli.json")
	out := flag.String("out", "dist", "output directory")
	flag.Parse()

	if err := build(*bin, *out); err != nil {
		log.Fatalf("site: %v", err)
	}
}

func build(bin, outDir string) error {
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	layout, err := template.ParseFiles("layout.html")
	if err != nil {
		return fmt.Errorf("reading layout: %w", err)
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			// Heading IDs let the specifications' own tables of contents work
			// as in-page links, exactly as they do on GitHub.
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			// The landing page contains a hand-written HTML figure. Nothing
			// here is user input; the sources are files in this repository.
			gmhtml.WithUnsafe(),
		),
	)

	all := pages()
	var plain []string

	for _, p := range all {
		source, err := os.ReadFile(p.Src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p.Src, err)
		}

		var body bytes.Buffer
		if err := md.Convert(source, &body); err != nil {
			return fmt.Errorf("rendering %s: %w", p.Src, err)
		}

		html := rewriteDocLinks(body.String())
		html = wrapTables(html)

		v := view{
			Title:          p.Title,
			Description:    p.Description,
			Out:            p.Out,
			BaseURL:        BaseURL,
			FormatVersion:  FormatVersion,
			NumberedByHand: p.NumberedByHand,
			Nav:            navFor(all, p),
			Content:        template.HTML(html),
		}

		var out bytes.Buffer
		if err := layout.Execute(&out, v); err != nil {
			return fmt.Errorf("templating %s: %w", p.Out, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, p.Out), out.Bytes(), 0o644); err != nil {
			return err
		}
		fmt.Printf("  %-22s %s\n", p.Out, humanSize(out.Len()))

		plain = append(plain, plainSection(p, string(source)))
	}

	if err := copyFile("style.css", filepath.Join(outDir, "style.css")); err != nil {
		return err
	}

	// GitHub Pages runs Jekyll unless told otherwise, which would drop files
	// whose names begin with an underscore. Nothing here starts with one, but
	// the marker also skips a pointless build step.
	if err := os.WriteFile(filepath.Join(outDir, ".nojekyll"), nil, 0o644); err != nil {
		return err
	}

	if err := writeLLMSIndex(outDir, all); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "llms-full.txt"),
		[]byte(strings.Join(plain, "\n\n")), 0o644); err != nil {
		return err
	}
	if err := writeCLIJSON(bin, outDir); err != nil {
		return err
	}

	fmt.Printf("  %-22s %s\n", "style.css", "")
	return checkLinks(outDir, all)
}

var (
	hrefAttr = regexp.MustCompile(`href="([^"]+)"`)
	idAttr   = regexp.MustCompile(`id="([^"]+)"`)
)

// checkLinks verifies that every internal link resolves.
//
// Links between the specifications are written as Markdown file references and
// rewritten on the way out, and the specifications' own tables of contents
// depend on generated heading IDs. Both are easy to break silently, so the
// build refuses to produce a site with a dead internal link.
func checkLinks(outDir string, all []page) error {
	ids := map[string]map[string]bool{}
	hrefs := map[string][]string{}

	for _, p := range all {
		body, err := os.ReadFile(filepath.Join(outDir, p.Out))
		if err != nil {
			return err
		}
		text := string(body)

		found := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(text, -1) {
			found[m[1]] = true
		}
		ids[p.Out] = found

		for _, m := range hrefAttr.FindAllStringSubmatch(text, -1) {
			hrefs[p.Out] = append(hrefs[p.Out], m[1])
		}
	}

	// Non-page files the site also publishes.
	sidecars := map[string]bool{
		"style.css": true, "llms.txt": true, "llms-full.txt": true, "cli.json": true,
	}

	var broken []string
	for from, list := range hrefs {
		for _, href := range list {
			if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") ||
				strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "data:") {
				continue
			}
			target, anchor := href, ""
			if i := strings.IndexByte(href, '#'); i >= 0 {
				target, anchor = href[:i], href[i+1:]
			}
			if target == "" {
				target = from // a bare #anchor points into this page
			}
			if sidecars[target] {
				continue
			}
			if _, ok := ids[target]; !ok {
				broken = append(broken, fmt.Sprintf("%s -> %s (no such page)", from, href))
				continue
			}
			if anchor != "" && !ids[target][anchor] {
				broken = append(broken, fmt.Sprintf("%s -> %s (no such anchor)", from, href))
			}
		}
	}

	if len(broken) > 0 {
		sort.Strings(broken)
		return fmt.Errorf("%d broken internal link(s):\n  %s", len(broken), strings.Join(broken, "\n  "))
	}
	fmt.Printf("  %-22s %d pages, no broken internal links\n", "link check", len(all))
	return nil
}

func navFor(all []page, current page) []navItem {
	var items []navItem
	for _, p := range all {
		if p.Nav == "" {
			continue
		}
		items = append(items, navItem{Href: p.Out, Label: p.Nav, Current: p.Out == current.Out})
	}
	return items
}

// docLink matches a Markdown-style link to a repository document, with an
// optional docs/ prefix and an optional anchor.
var docLink = regexp.MustCompile(`href="(?:\.\./)?(?:docs/)?([A-Za-z0-9._-]+)\.md(#[^"]*)?"`)

// rewriteDocLinks turns links between the repository's Markdown files into
// links between the rendered pages.
//
// The specifications cross-reference each other as format-v1.md and
// source-format.md so that they read correctly on GitHub and in a checkout.
// The site is the same text, so the links have to be translated rather than
// the sources rewritten.
func rewriteDocLinks(html string) string {
	return docLink.ReplaceAllStringFunc(html, func(m string) string {
		parts := docLink.FindStringSubmatch(m)
		name, anchor := parts[1], parts[2]
		switch name {
		case "README":
			return `href="index.html` + anchor + `"`
		case "format-v1", "source-format":
			return `href="` + name + ".html" + anchor + `"`
		}
		// Anything else is not a page on this site; point at the repository.
		return `href="https://github.com/pridkett/slidepack/blob/main/` + name + ".md" + anchor + `"`
	})
}

// tableTag finds a rendered table so it can be given a scroll container.
var tableTag = regexp.MustCompile(`(?s)<table>.*?</table>`)

// wrapTables puts each table in a horizontally scrollable box, so a wide
// diagnostic table scrolls inside itself instead of widening the whole page.
func wrapTables(html string) string {
	return tableTag.ReplaceAllStringFunc(html, func(t string) string {
		return `<div class="table-scroll">` + t + `</div>`
	})
}

// plainSection renders one page for llms-full.txt.
func plainSection(p page, source string) string {
	var b strings.Builder
	b.WriteString("<!-- ")
	b.WriteString(BaseURL + "/" + p.Out)
	b.WriteString(" -->\n\n")
	b.WriteString(strings.TrimSpace(source))
	b.WriteString("\n")
	return b.String()
}

// writeLLMSIndex writes /llms.txt.
//
// The convention (llmstxt.org) is a Markdown file with an H1, a blockquote
// summary and lists of annotated links. It exists so a model can find the
// authoritative text without crawling and de-chroming HTML.
func writeLLMSIndex(outDir string, all []page) error {
	var b strings.Builder
	b.WriteString("# slidepack\n\n")
	b.WriteString("> A command-line tool that compiles a presentation source directory into a single\n")
	b.WriteString("> self-contained .html file which opens from the filesystem with no server, no\n")
	b.WriteString("> browser extension and no network, and expands it back into the original\n")
	b.WriteString("> directory byte for byte. The directory is source; the HTML file is a build\n")
	b.WriteString("> artifact.\n\n")

	b.WriteString("Written in Go, distributed as a single static executable. The packed format is\n")
	b.WriteString("version 1: an HTML bootstrap, a JSON manifest, an inline first-party runtime,\n")
	b.WriteString("and exactly one base64(gzip(deterministic USTAR tar)) payload that the browser\n")
	b.WriteString("expands in memory.\n\n")

	b.WriteString("## Documentation\n\n")
	for _, p := range all {
		fmt.Fprintf(&b, "- [%s](%s/%s): %s\n", p.Nav, BaseURL, p.Out, p.Description)
	}

	b.WriteString("\n## Machine-readable\n\n")
	fmt.Fprintf(&b, "- [CLI interface](%s/cli.json): every command, option, exit code and "+
		"diagnostic code with a remedy, as JSON. The same document `slidepack help --json` prints.\n", BaseURL)
	fmt.Fprintf(&b, "- [Full text](%s/llms-full.txt): every page above, concatenated as Markdown.\n", BaseURL)

	b.WriteString("\n## Working with slidepack\n\n")
	b.WriteString("- Edit the source directory, never the packed .html file. Reading its base64\n")
	b.WriteString("  payload yields nothing useful; run `slidepack unpack` instead.\n")
	b.WriteString("- Run `slidepack validate --json <dir>` and match on the stable `code` field.\n")
	b.WriteString("  Exit 0 means valid, 3 means invalid, 2 is a usage error.\n")
	b.WriteString("- Bundle JavaScript into a classic script before packing; ES modules are\n")
	b.WriteString("  rejected. Inline data instead of fetching it; package paths do not exist at\n")
	b.WriteString("  runtime, only blob: URLs.\n")
	b.WriteString("- With `--json`, stdout carries only the document and messages go to stderr.\n")

	b.WriteString("\n## Optional\n\n")
	b.WriteString("- [Source repository](https://github.com/pridkett/slidepack)\n")

	return os.WriteFile(filepath.Join(outDir, "llms.txt"), []byte(b.String()), 0o644)
}

// writeCLIJSON publishes the binary's own interface description.
//
// Generating it rather than transcribing it means the site cannot document a
// flag the tool does not have.
func writeCLIJSON(bin, outDir string) error {
	if bin == "" {
		fmt.Println("  cli.json               skipped (-bin not given)")
		return nil
	}
	out, err := exec.Command(bin, "help", "--json").Output()
	if err != nil {
		return fmt.Errorf("running %s help --json: %w", bin, err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "cli.json"), out, 0o644); err != nil {
		return err
	}
	fmt.Printf("  %-22s %s\n", "cli.json", humanSize(len(out)))
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func humanSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KiB", float64(n)/1024)
}
