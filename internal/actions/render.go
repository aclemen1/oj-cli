package actions

import (
	"github.com/aclemen1/ordo-cli/internal/config"
	"github.com/aclemen1/ordo-cli/internal/spec"
	"github.com/aclemen1/ordo-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "doc", Name: "render", Top: true,
		Summary: "Render the agenda or the minutes of a sitting as Markdown, HTML, docx or PDF.",
		Discussion: "A frozen agenda and approved minutes come from the Markdown written by `sitting freeze` and `sitting minute`; " +
			"their docx or PDF is kept next to it in the store. Anything else is a draft, written to --out or to a temporary directory. " +
			"docx and PDF go through pandoc; the sphere's reference_doc sets the docx layout.",
		Params: []spec.Param{
			sittingArg(sittingOrAlias), sphereParam(),
			{Name: "doc", Kind: spec.String, Default: "agenda", Enum: store.Docs, Help: "Which document."},
			{Name: "to", Kind: spec.String, Default: "md", Enum: store.Formats, Help: "Format."},
			{Name: "out", Kind: spec.String, Help: "File to write. Defaults to next to the Markdown."},
		},
		Effects: []string{"Writes the document; a docx or PDF of a final document is committed in the store."},
		Examples: []string{
			"ordo render RDIR --sphere pro",
			"ordo render RDIR-2026-10-08 --doc minutes --to docx --sphere pro",
			"ordo render RDIR-2026-10-08 --to pdf --out ~/Desktop/odj.pdf --sphere pro",
		},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.RenderDoc(ctx.Str("sitting"), ctx.Str("doc"), ctx.Str("to"), config.Expand(ctx.Str("out")))
		}),
	})
}
