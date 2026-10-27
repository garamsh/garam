package collab

import (
	"fmt"
	"strings"
)

// Markdown renders a page's blocks as Markdown, as an AI reads a page
// and as an export writes one. Formatting inside text is not kept, as
// Block does not keep it.
func Markdown(blocks []Block) string {
	var out []string
	for _, b := range blocks {
		out = append(out, markdownOf(b))
	}
	return strings.Join(out, "\n\n") + "\n"
}

func markdownOf(b Block) string {
	attr := func(k string) string {
		if v, ok := b.Attrs[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	lines := func(prefix func(i int, it Item) string) string {
		var out []string
		for i, it := range b.Items {
			out = append(out, prefix(i, it)+it.Text)
		}
		return strings.Join(out, "\n")
	}
	switch b.Type {
	case "heading":
		level := 1
		_, _ = fmt.Sscan(attr("level"), &level)
		return strings.Repeat("#", max(1, min(level, 6))) + " " + b.Text
	case "bulletList":
		return lines(func(int, Item) string { return "- " })
	case "orderedList":
		return lines(func(i int, _ Item) string { return fmt.Sprintf("%d. ", i+1) })
	case "taskList":
		return lines(func(_ int, it Item) string {
			if it.Checked {
				return "- [x] "
			}
			return "- [ ] "
		})
	case "codeBlock":
		return "```" + attr("language") + "\n" + b.Text + "\n```"
	case "blockquote":
		return "> " + strings.ReplaceAll(b.Text, "\n", "\n> ")
	case "horizontalRule":
		return "---"
	case "image":
		return fmt.Sprintf("![%s](%s)", attr("alt"), attr("src"))
	}
	return b.Text
}
