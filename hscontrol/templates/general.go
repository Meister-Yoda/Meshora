package templates

import (
	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/assets"
)

// publicBody lays out an end-user page in the Meshora design: the brand
// above a centred card holding the content, and the footer below it.
// Styling lives in [assets.CSS].
func publicBody(children ...elem.Node) *elem.Element {
	return elem.Body(
		attrs.Props{attrs.Class: "public", "translate": "no"},
		elem.Raw(meshoraBackdrop),
		languageSwitcher(),
		elem.Header(attrs.Props{attrs.Class: "public-header"}, brand("")),
		elem.Main(attrs.Props{attrs.Class: "md-typeset public-card"}, children...),
		pageFooter(),
	)
}

// Styled Element Wrappers
// These functions wrap elem-go elements using CSS classes.
// Styling is handled by the CSS in [assets.CSS].

// H1 creates a H1 element styled by .md-typeset h1
func H1(children ...elem.Node) *elem.Element {
	return elem.H1(nil, children...)
}

// H2 creates a H2 element styled by .md-typeset h2
func H2(children ...elem.Node) *elem.Element {
	return elem.H2(nil, children...)
}

// H3 creates a H3 element styled by .md-typeset h3
func H3(children ...elem.Node) *elem.Element {
	return elem.H3(nil, children...)
}

// P creates a paragraph element styled by .md-typeset p
func P(children ...elem.Node) *elem.Element {
	return elem.P(nil, children...)
}

// Ol creates an ordered list element styled by .md-typeset ol
func Ol(children ...elem.Node) *elem.Element {
	return elem.Ol(nil, children...)
}

// Ul creates an unordered list element styled by .md-typeset ul
func Ul(children ...elem.Node) *elem.Element {
	return elem.Ul(nil, children...)
}

// A creates a link element styled by .md-typeset a
func A(href string, children ...elem.Node) *elem.Element {
	return elem.A(attrs.Props{attrs.Href: href}, children...)
}

// Code creates an inline code element styled by .md-typeset code
func Code(children ...elem.Node) *elem.Element {
	return elem.Code(nil, children...)
}

// codeBlockText creates a preformatted code block styled by
// .md-typeset pre > code.
func codeBlockText(code string) *elem.Element {
	return elem.Pre(nil, elem.Code(nil, elem.Text(code)))
}

// pageFooter creates a consistent footer for all pages.
func pageFooter() *elem.Element {
	return elem.Footer(attrs.Props{attrs.Class: "public-footer"}, poweredBy()...)
}

// page renders a standard end-user page: the given title in the document
// head and the content in the Meshora card layout (see [publicBody]).
func page(title string, content ...elem.Node) *elem.Element {
	return HtmlStructure(
		elem.Title(nil, elem.Text(title)),
		publicBody(content...),
	)
}

// HtmlStructure creates a complete HTML document structure with proper meta tags
// and semantic HTML5 structure. The head and body elements are passed as parameters
// to allow for customization of each page.
// Styling is provided by the Meshora stylesheet [assets.CSS], inlined; it uses
// the system font stack, so no third-party font requests are made.
func HtmlStructure(head, body *elem.Element) *elem.Element {
	return elem.Html(
		attrs.Props{attrs.Lang: "en"},
		elem.Head(
			nil,
			elem.Meta(attrs.Props{
				attrs.Charset: "UTF-8",
			}),
			elem.Meta(attrs.Props{
				attrs.HTTPequiv: "X-UA-Compatible",
				attrs.Content:   "IE=edge",
			}),
			elem.Meta(attrs.Props{
				attrs.Name:    "viewport",
				attrs.Content: "width=device-width, initial-scale=1.0",
			}),
			elem.Link(attrs.Props{
				attrs.Rel:  "icon",
				attrs.Href: "/favicon.ico?v=meshora",
			}),
			elem.Meta(attrs.Props{
				attrs.Name:    "color-scheme",
				attrs.Content: "light dark",
			}),
			elem.Style(attrs.Props{attrs.Type: "text/css"}, elem.Raw(assets.CSS)),
			head,
		),
		body,
	)
}

// BlankPage creates a minimal blank HTML page with favicon.
// Used for endpoints that need to return a valid HTML page with no content.
func BlankPage() *elem.Element {
	return elem.Html(
		attrs.Props{attrs.Lang: "en"},
		elem.Head(
			nil,
			elem.Meta(attrs.Props{
				attrs.Charset: "UTF-8",
			}),
			elem.Link(attrs.Props{
				attrs.Rel:  "icon",
				attrs.Href: "/favicon.ico?v=meshora",
			}),
		),
		elem.Body(nil),
	)
}
