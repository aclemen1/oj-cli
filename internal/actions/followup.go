package actions

import (
	"fmt"
	"io"
	"strconv"

	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "actions", Name: "ls",
		Summary: "List the actions decided in sittings, open first by due date.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Help: "Meeting alias. Defaults to every meeting."},
			sphereParam(),
			{Name: "who", Kind: spec.String, Help: "Keep the actions of this person."},
			{Name: "state", Kind: spec.String, Default: "open", Enum: []string{"open", "done", "all"}, Help: "Keep open, done or all actions."},
			{Name: "due-before", Kind: spec.String, Help: "Keep the actions due before this date, YYYY-MM-DD."},
		},
		Examples: []string{"oj actions ls RDIR --sphere pro", "oj actions ls --who Marie --due-before 2026-11-01 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.Actions(store.ActionFilter{Meeting: ctx.Str("meeting"), Who: ctx.Str("who"), State: ctx.Str("state"), DueBefore: ctx.Str("due-before")})
		}),
		Text: func(w io.Writer, r any) {
			for _, a := range r.([]store.ActionRow) {
				mark := "[ ]"
				if a.Done {
					mark = "[x]"
				}
				fmt.Fprintf(w, "%s %-10s %-12s %-10s#%d %s\n", mark, a.Due, a.Who, a.Item, a.N, a.What)
			}
		},
	})
	spec.Register(&spec.Action{
		Category: "actions", Name: "done",
		Summary: "Mark an action of an item's outcome done, or open again with --undo.",
		Params: []spec.Param{
			{Name: "id", Kind: spec.String, Positional: true, Required: true, Help: "Item id, e.g. RDIR-17."},
			{Name: "n", Kind: spec.String, Positional: true, Required: true, Help: "Number of the action, from 1, as `oj actions ls` shows it."},
			sphereParam(),
			{Name: "sitting", Kind: spec.String, Help: "Sitting of the outcome. Defaults to the latest outcome with actions."},
			{Name: "undo", Kind: spec.Bool, Help: "Mark the action open again."},
		},
		Effects:  []string{"Rewrites the item with the action marked done or open."},
		Examples: []string{"oj actions done RDIR-17 1 --sphere pro", "oj actions done RDIR-17 1 --undo --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			n, err := strconv.Atoi(ctx.Str("n"))
			if err != nil {
				return nil, spec.UserError("<n> takes a number, got %q. Example: oj actions done RDIR-17 1", ctx.Str("n"))
			}
			return st.SetActionDone(ctx.Str("id"), ctx.Str("sitting"), n, !ctx.Bool("undo"))
		}),
	})
}
