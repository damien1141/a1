package kamitool

import (
	"strings"
)

// parseMarkdownContent parses a simple markdown-like content string into
// sections. This is the LLM's friendlier interface: instead of producing
// nested JSON arrays of objects, the LLM writes natural markdown.
//
// Supported syntax:
//
//	# Heading         → section heading (level 1)
//	## Subheading     → section heading (level 2)
//	plain text        → body paragraph (accumulated until next heading or bullet)
//	- bullet item     → bullet point
//	<blank line>      → paragraph break
//
// The first # heading or the first line of text becomes the implicit
// title if no explicit title is set.
func parseMarkdownContent(content string) []docSection {
	var sections []docSection
	var current *docSection
	var bodyLines []string

	flushBody := func() {
		if current != nil && len(bodyLines) > 0 {
			text := strings.TrimSpace(strings.Join(bodyLines, " "))
			if text != "" {
				if current.Body != "" {
					current.Body += "\n\n" + text
				} else {
					current.Body = text
				}
			}
			bodyLines = nil
		}
	}

	lines := strings.Split(content, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		// Heading: # or ##
		if strings.HasPrefix(line, "# ") {
			flushBody()
			sections = append(sections, docSection{Heading: strings.TrimSpace(line[2:]), Level: 1})
			current = &sections[len(sections)-1]
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flushBody()
			sections = append(sections, docSection{Heading: strings.TrimSpace(line[3:]), Level: 2})
			current = &sections[len(sections)-1]
			continue
		}

		// Bullet: - or *
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			flushBody()
			if current == nil {
				sections = append(sections, docSection{Heading: "Overview", Level: 1})
				current = &sections[len(sections)-1]
			}
			current.Bullets = append(current.Bullets, strings.TrimSpace(line[2:]))
			continue
		}

		// Blank line: paragraph break
		if line == "" {
			flushBody()
			continue
		}

		// Body text
		if current == nil {
			sections = append(sections, docSection{Heading: "Overview", Level: 1})
			current = &sections[len(sections)-1]
		}
		bodyLines = append(bodyLines, line)
	}
	flushBody()

	return sections
}

// docSection is the parsed representation of one section.
type docSection struct {
	Heading string
	Body    string
	Bullets []string
	Level   int
}
