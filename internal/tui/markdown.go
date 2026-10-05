package tui

import (
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

type mdKey struct {
	md   string
	w    int
	dark bool
}

// mdCache keeps rendered Markdown: the screen redraws every second in the
// live view, and rendering is costly.
var mdCache = map[mdKey][]string{}

// markdown renders Markdown for the terminal at width w, in the shades of the
// terminal's background, without glamour's outer margins, as office's TUI does.
func markdown(md string, w int) []string {
	k := mdKey{md, w, darkBackground}
	if l, ok := mdCache[k]; ok {
		return l
	}
	cfg := styles.DarkStyleConfig
	if !darkBackground {
		cfg = styles.LightStyleConfig
	}
	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix, cfg.Document.BlockSuffix = "", ""
	for _, h := range []*gansi.StyleBlock{&cfg.H2, &cfg.H3, &cfg.H4, &cfg.H5, &cfg.H6} {
		h.Prefix = ""
	}
	var out []string
	if r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(w)); err == nil {
		if s, err := r.Render(md); err == nil {
			out = strings.Split(strings.Trim(s, "\n"), "\n")
		}
	}
	if out == nil {
		out = strings.Split(ansi.Wordwrap(md, w, ""), "\n")
	}
	if len(mdCache) > 200 {
		mdCache = map[mdKey][]string{}
	}
	mdCache[k] = out
	return out
}
