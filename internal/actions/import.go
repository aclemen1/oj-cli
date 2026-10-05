package actions

import (
	"io"
	"os"

	"github.com/aclemen1/oj-cli/internal/config"
	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "import", Name: "gtasks",
		Summary: "Turn the open tasks of a Google Tasks list into proposed items of a meeting.",
		Discussion: "Input: the JSON a task CLI prints for one list, {\"items\": [...]} or [...], e.g. " +
			"`gws tasks tasks list --params '{\"tasklist\": \"<id>\"}'`. oj does not call Google itself. " +
			"Each item keeps gtasks:<id> as a ref, and gmail:message/<id> for a task made from an e-mail; " +
			"a task already imported is skipped, so the import can be run again.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."},
			sphereParam(),
			{Name: "from", Kind: spec.String, Required: true, Help: "JSON file of the list, or - for standard input."},
			{Name: "with-completed", Kind: spec.Bool, Help: "Also import completed tasks."},
			{Name: "accept", Kind: spec.Bool, Help: "Put the items on the agenda at once instead of proposing them."},
			{Name: "dry-run", Kind: spec.Bool, Help: "Show the items without writing them."},
		},
		Effects:  []string{"Writes one item per open task, for the next planned sitting, in one commit."},
		Examples: []string{"oj import gtasks RDIR --from rdir-tasks.json --dry-run --sphere pro", "oj import gtasks RDIR --from - --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			var b []byte
			var err error
			if f := ctx.Str("from"); f == "-" {
				b, err = io.ReadAll(ctx.Stdin)
			} else {
				b, err = os.ReadFile(config.Expand(f))
			}
			if err != nil {
				return nil, spec.UserError("cannot read --from: %v", err)
			}
			tasks, err := store.ParseTasks(b)
			if err != nil {
				return nil, err
			}
			return st.ImportTasks(ctx.Str("meeting"), tasks, ctx.Bool("with-completed"), ctx.Bool("accept"), ctx.Bool("dry-run"))
		}),
	})
}
