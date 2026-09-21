package watcher

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	icon    = "https://static.wikia.nocookie.net/663e53f7-1e79-4906-95a7-2c1df4ebbada"
	unknown = "unknown"
)

type appearance struct {
	color int
	emoji string
}

var appearances = map[string]appearance{
	"discussion/forum/post":    {54998, "📝"},
	"discussion/forum/reply":   {54998, "📝"},
	"discussion/forum/poll":    {54998, "📝"},
	"discussion/forum/quiz":    {54998, "📝"},
	"discussion/wall/post":     {3752525, "✉️"},
	"discussion/wall/reply":    {3752525, "📩"},
	"discussion/comment/post":  {10802, "🗒️"},
	"discussion/comment/reply": {10802, "🗒️"},
	"unknown":                  {0, "❓"},
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

type webhookPayload struct {
	AllowedMentions allowedMentions `json:"allowed_mentions"`
	Content         string          `json:"content,omitempty"`
	Embeds          []embed         `json:"embeds,omitempty"`
}

type embedAuthor struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedImage struct {
	URL string `json:"url"`
}

type embed struct {
	Color       int          `json:"color"`
	Author      embedAuthor  `json:"author"`
	Title       string       `json:"title,omitempty"`
	URL         string       `json:"url,omitempty"`
	Description string       `json:"description,omitempty"`
	Fields      []embedField `json:"fields,omitempty"`
	Image       *embedImage  `json:"image,omitempty"`
	Footer      embedFooter  `json:"footer"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

func (e embed) length() int {
	n := len([]rune(e.Title)) + len([]rune(e.Description)) + len([]rune(e.URL)) +
		len([]rune(e.Footer.Text)) + len([]rune(e.Author.Name))
	for _, f := range e.Fields {
		n += len([]rune(f.Name)) + len([]rune(f.Value))
	}
	return n
}

func eventFor(container, funnel string, reply bool) string {
	switch container {
	case "FORUM":
		if reply {
			return "discussion/forum/reply"
		}
		switch funnel {
		case "POLL":
			return "discussion/forum/poll"
		case "QUIZ":
			return "discussion/forum/quiz"
		case "TEXT", "":
			return "discussion/forum/post"
		}
		return "unknown"
	case "WALL":
		if reply {
			return "discussion/wall/reply"
		}
		return "discussion/wall/post"
	case "ARTICLE_COMMENT":
		if reply {
			return "discussion/comment/reply"
		}
		return "discussion/comment/post"
	}
	return "unknown"
}

func (c *Config) message(p *post, page articleName) webhookPayload {
	container := p.containerType()
	event := eventFor(container, p.Funnel, p.IsReply)
	mentions := allowedMentions{Parse: []string{}}
	if c.Compact {
		return webhookPayload{
			AllowedMentions: mentions,
			Content:         appearances[event].emoji + " " + c.compactText(p, page),
		}
	}

	author := c.author(p)
	if author.IconURL == "" {
		author.IconURL = icon
	}
	e := embed{
		Color:     appearances[event].color,
		Author:    author,
		Footer:    embedFooter{Text: p.ForumName},
		Timestamp: time.Unix(p.CreationDate.EpochSecond, 0).UTC().Format(time.RFC3339),
	}
	if text, image := parseDoc(p.JsonModel, c.Wiki, c.ArticlePath); text != "" {
		if image != "" {
			text = strings.ReplaceAll(text, image, "")
			e.Image = new(embedImage{URL: image})
		}
		e.Description = text
	} else if p.RawContent != "" {
		e.Description = truncate(p.RawContent, 3600)
	}

	switch container {
	case "FORUM":
		e.URL = c.Wiki + "/f/" + p.ThreadID
		if p.IsReply {
			e.Title = "Replied to \"" + p.threadTitle() + "\""
			break
		}
		switch p.Funnel {
		case "POLL":
			e.Title = "Created a poll \"" + p.Title + "\""
			if p.Poll != nil {
				imagePoll := len(p.Poll.Answers) > 0 && p.Poll.Answers[0].Image != nil
				for i, answer := range p.Poll.Answers {
					field := embedField{Name: fmt.Sprintf("Option %d", i+1), Value: answer.Text, Inline: true}
					if imagePoll && answer.Image != nil {
						field.Name = answer.Text
						field.Value = fmt.Sprintf("__[View image](%s)__", resolveURL(c.Wiki, answer.Image.URL))
					}
					e.Fields = append(e.Fields, field)
				}
			}
		case "QUIZ":
			e.Title = "Created a quiz \"" + p.Title + "\""
		default:
			e.Title = "Created \"" + p.Title + "\""
		}
		if tags := p.threadTags(); len(tags) > 0 {
			links := make([]string, 0, len(tags))
			for _, tag := range tags {
				link := c.articleURL(tag.ArticleTitle)
				if tag.RelativeURL != "" {
					link = c.Wiki + tag.RelativeURL
				}
				links = append(links, fmt.Sprintf("[%s](%s)", tag.ArticleTitle, link))
			}
			value := strings.Join(links, ", ")
			if len([]rune(value)) > 1000 {
				value = fmt.Sprintf("%d tags", len(tags))
			}
			e.Fields = append(e.Fields, embedField{Name: "Tags", Value: value})
		}
	case "WALL":
		wall := strings.TrimSuffix(p.ForumName, " Message Wall")
		e.URL = c.Wiki + "/f/" + p.ThreadID
		if p.IsReply {
			e.Title = "Replied to \"" + p.threadTitle() + "\" on " + wall + "'s Message Wall"
		} else {
			e.Title = "Created \"" + p.Title + "\" on " + wall + "'s Message Wall"
		}
	case "ARTICLE_COMMENT":
		e.URL = c.Wiki + "/f/" + p.ThreadID
		e.Footer.Text = unknown
		if page.Title != "" {
			e.Footer.Text = page.Title
		}
		if p.IsReply {
			e.Title = "Replied to a comment on " + e.Footer.Text
		} else {
			e.Title = "Commented on " + e.Footer.Text
		}
	}
	e.Title = truncate(e.Title, 254)
	return webhookPayload{AllowedMentions: mentions, Embeds: []embed{e}}
}

func (c *Config) compactText(p *post, page articleName) string {
	author := c.author(p)
	who := fmt.Sprintf("[%s](<%s>)", author.Name, author.URL)
	thread := c.Wiki + "/f/" + p.ThreadID
	switch p.containerType() {
	case "FORUM":
		if p.IsReply {
			return fmt.Sprintf("%s created a [reply](<%s>) to [%s](<%s>) in %s",
				who, thread, p.threadTitle(), thread, p.ForumName)
		}
		verb := "created"
		switch p.Funnel {
		case "POLL":
			verb = "created a poll"
		case "QUIZ":
			verb = "created a quiz"
		}
		return fmt.Sprintf("%s %s [%s](<%s>) in %s", who, verb, p.Title, thread, p.ForumName)
	case "WALL":
		user := strings.TrimSuffix(p.ForumName, " Message Wall")
		wall := c.articleURL("Message_Wall:" + user)
		if p.IsReply {
			return fmt.Sprintf("%s created a [reply](<%s>) to [%s](<%s>) on [%s's Message Wall](<%s>)",
				who, thread, p.threadTitle(), thread, user, wall)
		}
		return fmt.Sprintf("%s created [%s](<%s>) on [%s's Message Wall](<%s>)",
			who, p.Title, thread, user, wall)
	case "ARTICLE_COMMENT":
		title, article := unknown, c.Wiki
		if page.Title != "" {
			title, article = page.Title, c.Wiki+page.RelativeURL
		}
		if p.IsReply {
			return fmt.Sprintf("%s created a [reply](<%s>) to a [comment](<%s>) on [%s](<%s>)",
				who, thread, thread, title, article)
		}
		return fmt.Sprintf("%s created a [comment](<%s>) on [%s](<%s>)", who, thread, title, article)
	}
	return ""
}

// for anons
func (c *Config) author(p *post) embedAuthor {
	name := p.CreatedBy.Name
	var link string
	if ip := strings.TrimPrefix(p.CreatorIP, ">"); ip != "" {
		link = c.articleURL("Special:Contributions/" + ip)
		if c.HideIPs {
			name = "Unregistered user"
		} else {
			name = ip
		}
	} else if name != "" {
		link = c.articleURL("User:" + name)
	} else if p.CreatorID != "" {
		name = unknown
		link = c.Wiki + "/f/u/" + p.CreatorID
	} else {
		name = unknown
	}
	return embedAuthor{Name: name, URL: link, IconURL: resolveURL(c.Wiki, p.CreatedBy.AvatarURL)}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func pathName(title string) string {
	// %2F is unescaped so subpages stay path segments.
	return strings.ReplaceAll(url.PathEscape(strings.ReplaceAll(title, " ", "_")), "%2F", "/")
}

// articleURL builds a wiki URL from the siteinfo article path (e.g. /w/$1).
func articleURL(base, articlePath, title string) string {
	if articlePath == "" {
		articlePath = "/wiki/$1"
	}
	link := strings.ReplaceAll(articlePath, "$1", pathName(title))
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	return base + link
}

func (c *Config) articleURL(title string) string {
	return articleURL(c.Wiki, c.ArticlePath, title)
}

func resolveURL(base, ref string) string {
	switch {
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		return ref
	case strings.HasPrefix(ref, "//"):
		return "https:" + ref
	case strings.HasPrefix(ref, "/"):
		return base + ref
	case ref != "":
		return base + "/" + ref
	}
	return ""
}

// editor stores either a URL or a page title
// hence bare values become article paths
func resolveHref(base, articlePath, href string) string {
	if strings.HasPrefix(href, "/") || strings.Contains(href, "://") || strings.HasPrefix(href, "//") {
		return resolveURL(base, href)
	}
	if href == "" {
		return ""
	}
	return articleURL(base, articlePath, href)
}
