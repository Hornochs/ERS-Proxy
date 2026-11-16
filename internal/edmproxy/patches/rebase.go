package patches

import (
	"regexp"
	"strings"
)

// Absolute paths of web applications that are not written for a path prefix
// (RouterOS's WebFig). In HTML: src, href and action attributes. In HTML and
// scripts: navigations through location ("location.href = '/…'",
// "location.replace(`/…`)", "location.assign(…)", "location = …").
var (
	rebaseAttr = regexp.MustCompile(`(\s(?:src|href|action)\s*=\s*)(["'])/`)
	rebaseNav  = regexp.MustCompile("(location(?:\\.href)?\\s*=\\s*|location\\.(?:replace|assign)\\(\\s*)([\"'`])/")
)

// Rebase maps absolute paths ("/x") in body below base ("/d/{token}/") and
// returns the number of replacements. Protocol-relative URLs ("//host") and
// paths already below base stay unchanged. Attributes are only rewritten in
// HTML documents (html); navigations in documents and scripts.
func Rebase(body, base string, html bool) (string, int) {
	n := 0
	rewrite := func(re *regexp.Regexp, s string) string {
		var b strings.Builder
		last := 0
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			end := m[1] // just after the "/"
			rest := s[end:]
			if strings.HasPrefix(rest, "/") || strings.HasPrefix("/"+rest, base) {
				continue
			}
			b.WriteString(s[last : end-1])
			b.WriteString(base)
			last = end
			n++
		}
		if last == 0 {
			return s
		}
		b.WriteString(s[last:])
		return b.String()
	}
	if html {
		body = rewrite(rebaseAttr, body)
	}
	body = rewrite(rebaseNav, body)
	return body, n
}
