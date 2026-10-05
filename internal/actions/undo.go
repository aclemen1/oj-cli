package actions

import (
	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "sitting", Name: "unhold",
		Summary:  "Take back a hold: the sitting is frozen again, or planned when its agenda was never frozen.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam()},
		Effects:  []string{"Rewrites the sitting's state; outcomes recorded meanwhile stay."},
		Examples: []string{"oj sitting unhold PSEC-2026-10-08 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.UnholdSitting(ctx.Str("sitting"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "unminute",
		Summary: "Take back approved minutes: the sitting is held again and its items are back as before the minutes.",
		Discussion: "Items done or deferred by the minutes are back on the sitting's agenda; outcomes written by an agent are drafts again; " +
			"the rendered minutes are removed from the store (the VCS keeps them). Refused when an item changed since the minutes. " +
			"Confirm with the user first.",
		Params:      []spec.Param{sittingArg("Sitting id."), sphereParam()},
		Effects:     []string{"Rewrites the sitting and its items; removes the rendered minutes."},
		Destructive: true,
		Examples:    []string{"oj sitting unminute PSEC-2026-10-08 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.UnminuteSitting(ctx.Str("sitting"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "restore",
		Summary:  "Take back a cancellation: the sitting is planned again. Items moved away by the cancellation stay where they went.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam()},
		Effects:  []string{"Marks the sitting planned again."},
		Examples: []string{"oj sitting restore PSEC-2026-10-15 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.RestoreSitting(ctx.Str("sitting"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "restore",
		Summary: "Take back a drop (proposed again, accepted with --accept) or a done (on the agenda again).",
		Params: []spec.Param{itemArg(), sphereParam(),
			{Name: "accept", Kind: spec.Bool, Help: "For a dropped item: put it on the agenda at once."}},
		Effects:  []string{"Rewrites the item, on its sitting when still planned, else on the next planned one."},
		Examples: []string{"oj item restore PSEC-3 --sphere pro", "oj item restore PSEC-3 --accept --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.RestoreItem(ctx.Str("id"), ctx.Bool("accept"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "create-ref",
		Summary: "Create a target for an item with the sphere's command (e.g. an office dossier) and add its ref.",
		Discussion: "Runs refs.<scheme>.create of the sphere with {title}, {item}, {meeting}, {meeting_ref}, {sitting}, {notes}, {expected}; " +
			"reads the new id, adds <scheme>:<id> to the item, then runs refs.<scheme>.link with {id}. " +
			"An item that already has a ref of the scheme keeps it (known: true). Create a dossier only when the item needs follow-up.",
		Params: []spec.Param{itemArg(), sphereParam(),
			{Name: "scheme", Kind: spec.String, Help: "Ref scheme whose create command to run. Defaults to the only scheme with one."}},
		Effects:  []string{"Runs the sphere's create and link commands; adds the ref to the item."},
		Examples: []string{"oj item create-ref PSEC-3 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.CreateRef(ctx.Str("id"), ctx.Str("scheme"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "undefer",
		Summary:  "Take back a deferral: the item goes back to the sitting it was deferred from when still open, and is accepted.",
		Params:   []spec.Param{itemArg(), sphereParam()},
		Effects:  []string{"Rewrites the item's sitting and state."},
		Examples: []string{"oj item undefer PSEC-3 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.UndeferItem(ctx.Str("id"))
		}),
	})
}
