package style

import (
	"regexp"
	"strings"
)

var fontFamily = regexp.MustCompile(`font-family\s*:\s*([^;}]*[^;}\s])`)

// SystemFonts gives a style sheet a Mac's own fonts for the families
// inua's styles name, which a Mac does not have: each font-family list
// becomes the system's monospace (a list naming one), serif (a list
// ending in serif) or sans-serif. CoreText falls back for CJK as the
// system does.
func SystemFonts(css string) string {
	return fontFamily.ReplaceAllStringFunc(css, func(decl string) string {
		list := strings.ToLower(fontFamily.FindStringSubmatch(decl)[1])
		switch {
		case list == "inherit":
			return decl
		case strings.Contains(list, "mono"):
			return `font-family: "SF Mono", Menlo, monospace`
		case strings.HasSuffix(list, " serif") || strings.HasSuffix(list, ",serif"):
			return `font-family: "New York", "Hiragino Mincho ProN", serif`
		}
		return `font-family: ".AppleSystemUIFont", "Helvetica Neue", "Hiragino Sans", sans-serif`
	})
}
