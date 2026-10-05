package actions

import (
	"fmt"
	"io"

	"github.com/aclemen1/ordo-cli/internal/spec"
	"github.com/aclemen1/ordo-cli/internal/store"
)

func itemArg() spec.Param {
	return spec.Param{Name: "id", Kind: spec.String, Positional: true, Required: true, Help: "Item id, e.g. RDIR-17."}
}

func itemInput(ctx *spec.Context) store.ItemInput {
	return store.ItemInput{Title: ctx.Str("title"), Owner: ctx.Str("owner"), Kind: ctx.Str("kind"),
		Duration: ctx.Str("duration"), Expected: ctx.Str("expected"), Notes: ctx.Str("notes"),
		Attach: ctx.List("attach"), Refs: ctx.List("ref")}
}

func itemFields() []spec.Param {
	return []spec.Param{
		{Name: "owner", Kind: spec.String, Help: "Who brings the item."},
		{Name: "kind", Kind: spec.String, Enum: store.Kinds, Help: "What the meeting does with it. Defaults to the meeting's, then discussion."},
		{Name: "duration", Kind: spec.String, Help: "Planned length, e.g. 15m. Defaults to the meeting's item duration."},
		{Name: "expected", Kind: spec.String, Help: "For a decision: the question put to the meeting."},
		{Name: "attach", Kind: spec.StringList, Help: "Attachment (repeatable): artefact://<sphere>/<id>, a path or a URL."},
		{Name: "ref", Kind: spec.StringList, Help: "Where the item comes from or is followed (repeatable), e.g. office:U-0042."},
		{Name: "notes", Kind: spec.String, Help: "Free notes, kept in the item's body."},
	}
}

func textItems(w io.Writer, r any) {
	for _, it := range r.([]*store.Item) {
		owner := ""
		if it.Owner != "" {
			owner = " — " + it.Owner
		}
		sit := it.Sitting
		if sit == "" {
			sit = "-"
		}
		fmt.Fprintf(w, "%-10s %-9s %-18s %-5s %s%s\n", it.ID, it.State, sit, it.Duration, it.Title, owner)
	}
}

func registerItems() {
	spec.Register(&spec.Action{
		Category: "item", Name: "add",
		Summary: "Propose an item for a meeting; with --accept, put it on the agenda at once.",
		Discussion: "Without --sitting the item goes to the next planned sitting. Pass --accept when the user asks to add the item " +
			"to the agenda; without it the chair accepts it later. Returns the item with its id.",
		Params: append([]spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias, e.g. RDIR."},
			{Name: "title", Kind: spec.String, Positional: true, Required: true, Help: "One line."},
			sphereParam(),
			{Name: "accept", Kind: spec.Bool, Help: "Put the item on the agenda at once."},
			{Name: "sitting", Kind: spec.String, Help: "Planned sitting to put it in, e.g. RDIR-2026-10-15. Defaults to the next one."},
		}, itemFields()...),
		Effects: []string{"Writes the item with the next number of its meeting, proposed or accepted."},
		Examples: []string{
			`ordo item add RDIR "Budget 2027" --owner Marie --kind decision --duration 20m --expected "Approve the draft budget?" --sphere pro`,
			`ordo item add RDIR "Audit des accès S3" --ref office:U-0042 --accept --sphere pro`,
		},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			in := itemInput(ctx)
			in.Title = ctx.Str("title")
			return st.AddItem(ctx.Str("meeting"), in, ctx.Bool("accept"), ctx.Str("sitting"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "ls",
		Summary: "List items across meetings or in one.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Help: "Meeting alias. Defaults to every meeting."},
			sphereParam(),
			{Name: "state", Kind: spec.String, Default: "open", Enum: append([]string{"open", "all"}, store.ItemStates...), Help: "open: proposed, accepted and deferred."},
			{Name: "owner", Kind: spec.String, Help: "Keep the items of this owner."},
			{Name: "ref", Kind: spec.String, Help: "Keep the items with this ref, e.g. office:U-0042."},
			{Name: "sitting", Kind: spec.String, Help: "Keep the items planned for this sitting."},
			{Name: "search", Kind: spec.String, Help: "Keep the items whose title, question or notes contain this text."},
		},
		Examples: []string{"ordo item ls RDIR --sphere pro", "ordo item ls --ref office:U-0042 --state all --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.Items(store.ItemFilter{Meeting: ctx.Str("meeting"), State: ctx.Str("state"), Owner: ctx.Str("owner"),
				Ref: ctx.Str("ref"), Sitting: ctx.Str("sitting"), Search: ctx.Str("search")})
		}),
		Text: textItems,
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "show",
		Summary: "Show an item: fields, history across sittings with outcomes, log.",
		Discussion: "With --with-refs, each ref whose scheme the sphere's configuration knows (refs: under the sphere) " +
			"comes with what its command says about the target, e.g. the dossier behind office:U-0042.",
		Params:   []spec.Param{itemArg(), sphereParam(), {Name: "with-refs", Kind: spec.Bool, Help: "Add a summary of each ref's target."}},
		Examples: []string{"ordo item show RDIR-17 --sphere pro", "ordo item show RDIR-17 --with-refs --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			it, err := st.Item(ctx.Str("id"))
			if err != nil || !ctx.Bool("with-refs") {
				return it, err
			}
			shown := []store.RefShown{}
			for _, r := range it.Refs {
				if st.CanShowRef(r) {
					shown = append(shown, st.ShowRef(r))
				}
			}
			return map[string]any{"item": it, "refs": shown}, nil
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "edit",
		Summary: "Change fields of an item; attachments and refs are added, --clear-ref removes a ref; --notes replaces the notes.",
		Params: append(append([]spec.Param{itemArg(), sphereParam(), {Name: "title", Kind: spec.String, Help: "New title."}}, itemFields()...),
			spec.Param{Name: "clear-ref", Kind: spec.StringList, Help: "Ref to remove (repeatable), e.g. office:U-0042."}),
		Effects: []string{"Rewrites the item and logs the fields changed."},
		Examples: []string{"ordo item edit RDIR-17 --duration 30m --sphere pro", "ordo item edit RDIR-17 --attach artefact://pro/01JB2X5Q8 --sphere pro",
			"ordo item edit RDIR-17 --notes \"Contexte repris du dossier.\" --clear-ref office:U-0042 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			in := itemInput(ctx)
			in.ClearRefs = ctx.List("clear-ref")
			return st.EditItem(ctx.Str("id"), in)
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "accept",
		Summary:  "Put proposed items on the agenda of their sitting.",
		Params:   []spec.Param{{Name: "id", Kind: spec.StringList, Positional: true, Required: true, Help: "Item ids (several allowed)."}, sphereParam()},
		Effects:  []string{"Marks each proposed item accepted; an item already accepted stays as it is."},
		Examples: []string{"ordo item accept RDIR-17 RDIR-18 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.AcceptItems(ctx.List("id"))
		}),
		Text: textItems,
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "defer",
		Summary:  "Plan an item for a later sitting.",
		Params:   []spec.Param{itemArg(), sphereParam(), {Name: "to", Kind: spec.String, Help: "Planned sitting, e.g. RDIR-2026-10-22. Defaults to the one after the item's sitting."}},
		Effects:  []string{"Moves the item to the later sitting; an accepted item becomes deferred, a proposal stays proposed."},
		Examples: []string{"ordo item defer RDIR-17 --sphere pro", "ordo item defer RDIR-17 --to RDIR-2026-11-05 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.DeferItem(ctx.Str("id"), ctx.Str("to"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "drop",
		Summary:  "Withdraw an item, with a reason.",
		Params:   []spec.Param{itemArg(), sphereParam(), {Name: "reason", Kind: spec.String, Required: true, Help: "Why, kept on the item."}},
		Effects:  []string{"Marks the item dropped; it leaves every agenda."},
		Examples: []string{`ordo item drop RDIR-17 --reason "settled by e-mail" --sphere pro`},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.DropItem(ctx.Str("id"), ctx.Str("reason"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "item", Name: "order",
		Summary:  "Set the agenda order of a planned sitting.",
		Params:   []spec.Param{sittingArg(sittingOrAlias), {Name: "id", Kind: spec.StringList, Positional: true, Required: true, Help: "Item ids in the new order; the others follow in their current order."}, sphereParam()},
		Effects:  []string{"Writes the order on the sitting."},
		Examples: []string{"ordo item order RDIR RDIR-18 RDIR-17 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.OrderItems(ctx.Str("sitting"), ctx.List("id"))
		}),
		Text: func(w io.Writer, r any) { textAgenda(w, r.(*store.Agenda)) },
	})
	spec.Register(&spec.Action{
		Category: "outcome", Name: "set",
		Summary: "Record what came out of an item in its sitting.",
		Discussion: "An agent passes --by agent:<name>: its outcome stays a draft until `ordo sitting minute`. " +
			"A second call for the same item and sitting replaces the outcome.",
		Params: []spec.Param{
			itemArg(), sphereParam(),
			{Name: "summary", Kind: spec.String, Help: "Short text for the minutes."},
			{Name: "decision", Kind: spec.String, Help: "The decision taken."},
			{Name: "action", Kind: spec.StringList, Help: "Action \"what|who|due\" (repeatable), due as YYYY-MM-DD."},
			{Name: "next", Kind: spec.String, Default: "done", Enum: []string{"done", "deferred"}, Help: "What becomes of the item when the minutes are approved."},
			{Name: "by", Kind: spec.String, Help: "Who writes it: a name, or agent:<name> for a draft. Defaults to $ORDO_BY, then user."},
			{Name: "sitting", Kind: spec.String, Help: "Sitting of the outcome. Defaults to the item's."},
		},
		Effects: []string{"Writes the outcome in the item's history for the sitting, approved or draft."},
		Examples: []string{
			`ordo outcome set RDIR-17 --decision "Draft budget approved" --action "Send it to the rectorate|Marie|2026-10-20" --sphere pro`,
			`ordo outcome set RDIR-18 --summary "Not reached" --next deferred --by agent:claude --sphere pro`,
		},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.SetOutcome(ctx.Str("id"), ctx.Str("sitting"), store.OutcomeInput{Summary: ctx.Str("summary"),
				Decision: ctx.Str("decision"), Next: ctx.Str("next"), By: ctx.Str("by"), Actions: ctx.List("action")})
		}),
	})
}
