package records

import (
	"html"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Rich text (D-75): notes, task comments and descriptions are HTML from the editor
// (headings, lists, checklists, quotes, code, links, images, @mentions). The server keeps
// only safe tags and attributes; plain text from before stays plain text.

var richPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "h1", "h2", "h3", "strong", "b", "em", "i", "u", "s", "code", "pre", "blockquote",
		"ul", "ol", "li", "hr", "label", "div")
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoFollowOnLinks(true)
	// Images: files uploaded to the CRM (relative) or https.
	p.AllowAttrs("src").Matching(regexp.MustCompile(`^(/api/crm/v1/[A-Za-z0-9/_.\-]+(\?inline=1)?|https://[^\s"'<>]+)$`)).OnElements("img")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowRelativeURLs(true)
	// Checklists.
	p.AllowAttrs("data-type").Matching(regexp.MustCompile(`^(taskList|taskItem|mention)$`)).OnElements("ul", "li", "span")
	p.AllowAttrs("data-checked").Matching(regexp.MustCompile(`^(true|false)$`)).OnElements("li")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	// Mentions: <span data-type="mention" data-id="uuid" data-label="Name">@Name</span>
	p.AllowAttrs("data-id").Matching(regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)).OnElements("span")
	p.AllowAttrs("data-label").Matching(regexp.MustCompile(`^[^<>"]{1,80}$`)).OnElements("span")
	p.AllowAttrs("start").Matching(regexp.MustCompile(`^[0-9]{1,6}$`)).OnElements("ol")
	return p
}()

// isRichHTML: editor output starts with a tag; older plain-text values don't.
func isRichHTML(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "<") }

// sanitizeRich cleans editor HTML; plain text is kept as it is.
func sanitizeRich(s string) string {
	s = strings.TrimSpace(s)
	if !isRichHTML(s) {
		return s
	}
	out := strings.TrimSpace(richPolicy.Sanitize(s))
	if richPlain(out) == "" && !strings.Contains(out, "<img") {
		return ""
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]*>`)
var blockEndRe = regexp.MustCompile(`(?i)</(p|h1|h2|h3|li|blockquote|pre|div)>|<br\s*/?>`)
var spacesRe = regexp.MustCompile(`[ \t]+`)

// richPlain turns rich text into plain text (notifications, previews, search).
func richPlain(s string) string {
	if !isRichHTML(s) {
		return strings.TrimSpace(s)
	}
	s = blockEndRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(spacesRe.ReplaceAllString(l, " ")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

var mentionSpanRe = regexp.MustCompile(`data-type="mention"[^>]*data-id="([0-9a-fA-F-]{36})"|data-id="([0-9a-fA-F-]{36})"[^>]*data-type="mention"`)

// mentionIDs finds the people @mentioned in a note (rich or the older @[Name](id) text).
func mentionIDs(body string) []string {
	ids := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] && len(ids) < 10 {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, m := range mentionSpanRe.FindAllStringSubmatch(body, 20) {
		add(m[1])
		add(m[2])
	}
	for _, m := range mentionRe.FindAllStringSubmatch(body, 20) {
		add(m[2])
	}
	return ids
}
