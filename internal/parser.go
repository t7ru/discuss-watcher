package watcher

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"strings"
)

type docAttrs struct {
	ID       attrID `json:"id"`
	Name     string `json:"name"`
	Src      string `json:"src"`
	Href     string `json:"href"`
	UserID   attrID `json:"userId"`
	URL      string `json:"url"`
	WasAdded bool   `json:"wasAddedWithInlineLink"`
}

// attrID accepts both the number and the string form of an id
type attrID string

func (a *attrID) UnmarshalJSON(data []byte) error {
	if len(data) > 1 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = attrID(s)
		return nil
	}
	*a = attrID(data)
	return nil
}

type docMark struct {
	Type  string   `json:"type"`
	Attrs docAttrs `json:"attrs"`
}

type docNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Marks   []docMark `json:"marks"`
	Attrs   docAttrs  `json:"attrs"`
	Content []docNode `json:"content"`
}

type docParser struct {
	base        string
	articlePath string
	image       string
	items       int
}

func parseDoc(jsonModel, base, articlePath string) (string, string) {
	trimmed := strings.TrimSpace(jsonModel)
	if !strings.HasPrefix(trimmed, "{") {
		return "", ""
	}
	var doc docNode
	if json.Unmarshal([]byte(trimmed), &doc) != nil || doc.Type != "doc" {
		return "", ""
	}
	p := docParser{base: base, articlePath: articlePath}
	var out strings.Builder
	p.parse(&out, doc.Content, "")
	return truncate(out.String(), 2000), p.image
}

func (p *docParser) parse(out *strings.Builder, nodes []docNode, list string) {
	for _, n := range nodes {
		switch list {
		case "bulletlist":
			out.WriteString("\t• ")
		case "orderedlist":
			p.items++
			fmt.Fprintf(out, "\t%d. ", p.items)
		}
		kind := strings.ReplaceAll(n.Type, "_", "")
		switch kind {
		case "text":
			switch {
			case len(n.Marks) > 0:
				pre, suf := markWrappers(n.Marks, p.base, p.articlePath)
				out.WriteString(pre)
				out.WriteString(sanitizeMarkdown(n.Text))
				out.WriteString(suf)
			case list == "codeblock":
				out.WriteString(n.Text)
			default:
				out.WriteString(sanitizeMarkdown(n.Text))
			}
		case "paragraph":
			p.parse(out, n.Content, "paragraph")
			out.WriteByte('\n')
		case "bulletlist", "orderedlist":
			if kind == "orderedlist" {
				p.items = 0
			}
			p.parse(out, n.Content, kind)
		case "listitem":
			p.parse(out, n.Content, "listitem")
		case "codeblock":
			var code strings.Builder
			p.parse(&code, n.Content, "codeblock")
			out.WriteString("```\n")
			out.WriteString(strings.TrimRight(code.String(), "\n"))
			out.WriteString("\n```\n")
		case "blockquote":
			var quote strings.Builder
			p.parse(&quote, n.Content, "blockquote")
			for line := range strings.Lines(quote.String()) {
				out.WriteString("> ")
				out.WriteString(strings.TrimRight(line, "\r\n"))
				out.WriteByte('\n')
			}
		case "hardbreak":
			out.WriteByte('\n')
		case "image":
			if src := resolveURL(p.base, n.Attrs.Src); src != "" {
				out.WriteString(src)
				out.WriteByte('\n')
				p.image = src
			}
		case "mention":
			if n.Attrs.Name != "" {
				fmt.Fprintf(out, "[@%s](<%s>)", n.Attrs.Name, articleURL(p.base, p.articlePath, "User:"+n.Attrs.Name))
			}
		case "openGraph":
			if !n.Attrs.WasAdded && n.Attrs.URL != "" {
				out.WriteString(n.Attrs.URL)
				out.WriteByte('\n')
			}
		}
	}
}

func markWrappers(marks []docMark, base, articlePath string) (string, string) {
	var pre, suf string
	for _, mark := range marks {
		switch mark.Type {
		case "mention":
			pre += "["
			suf = "](" + base + "/f/u/" + string(mark.Attrs.UserID) + ")" + suf
		case "strong":
			pre += "**"
			suf = "**" + suf
		case "link":
			pre += "["
			suf = "](" + resolveHref(base, articlePath, mark.Attrs.Href) + ")" + suf
		case "em":
			pre += "_"
			suf = "_" + suf
		case "code":
			pre += "`"
			suf = "`" + suf
		}
	}
	return pre, suf
}

var (
	markdownChars = regexp.MustCompile("[`_*~<>{}@|]")
	subtextLine   = regexp.MustCompile(`(?m)^-# `)
	headerLine    = regexp.MustCompile(`(?m)^#+ `)
	listLine      = regexp.MustCompile(`(?m)^(\s*)- `)
	numberedLine  = regexp.MustCompile(`(?m)^(\s*\d+)\. `)
)

// post bodies cannot smuggle Discord formatting
func sanitizeMarkdown(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "//", "/\\/")
	text = strings.ReplaceAll(text, "](", `]\(`)
	text = markdownChars.ReplaceAllString(text, `\$0`)
	if strings.Count(text, ":") > 1 {
		text = strings.ReplaceAll(text, ":", `\:`)
	}
	text = subtextLine.ReplaceAllString(text, `\$0`)
	text = headerLine.ReplaceAllString(text, `\$0`)
	text = listLine.ReplaceAllString(text, `$1\- `)
	text = numberedLine.ReplaceAllString(text, `$1\. `)
	return text
}
