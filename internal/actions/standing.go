package actions

import (
	"fmt"
	"io"

	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "standing", Name: "add",
		Summary: "Declare a recurring item of a meeting: every sitting gets its own instance, accepted, at the start or the end.",
		Discussion: "Until someone acts on it, a sitting's instance is virtual: shown in the agenda (virtual: true, standing: <key>) but not written. " +
			"It is written at the first change, by `oj standing apply`, or at freeze or hold. Not dealt with in a sitting, it is dropped at the minutes (not deferred).",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."},
			{Name: "title", Kind: spec.String, Positional: true, Required: true, Help: "Title of the item."},
			sphereParam(),
			{Name: "key", Kind: spec.String, Help: "Short key, e.g. repas. Defaults to one made from the title."},
			{Name: "place", Kind: spec.String, Default: "end", Enum: store.Places, Help: "Where it goes in the agenda."},
			{Name: "kind", Kind: spec.String, Enum: store.Kinds, Help: "Kind. Defaults to the meeting's."},
			{Name: "duration", Kind: spec.String, Help: "Planned length, e.g. 5m. Defaults to the meeting's item duration."},
			{Name: "expected", Kind: spec.String, Help: "The question put to the meeting."},
			{Name: "notes", Kind: spec.String, Help: "Notes copied into each instance."},
		},
		Effects:  []string{"Adds the recurring item to meeting.md."},
		Examples: []string{`oj standing add RDIR "Date de la prochaine séance" --place end --duration 5m --sphere pro`},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.AddStanding(ctx.Str("meeting"), store.Standing{Key: ctx.Str("key"), Title: ctx.Str("title"), Place: ctx.Str("place"),
				Kind: ctx.Str("kind"), Duration: ctx.Str("duration"), Expected: ctx.Str("expected"), Notes: ctx.Str("notes")})
		}),
	})
	spec.Register(&spec.Action{
		Category: "standing", Name: "ls",
		Summary:  "List the recurring items of a meeting.",
		Params:   []spec.Param{{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."}, sphereParam()},
		Examples: []string{"oj standing ls RDIR --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			m, err := st.Meeting(ctx.Str("meeting"))
			if err != nil {
				return nil, err
			}
			if m.Standing == nil {
				return []store.Standing{}, nil
			}
			return m.Standing, nil
		}),
		Text: func(w io.Writer, r any) {
			for _, s := range r.([]store.Standing) {
				fmt.Fprintf(w, "%-16s %-5s %-5s %s\n", s.Key, s.Place, s.Duration, s.Title)
			}
		},
	})
	spec.Register(&spec.Action{
		Category: "standing", Name: "from",
		Summary: "Make an item recurring: declare a recurring item from its title, kind, duration, question and notes; the item becomes its instance.",
		Params: []spec.Param{itemArg(), sphereParam(),
			{Name: "place", Kind: spec.String, Default: "end", Enum: store.Places, Help: "Where it goes in the agendas to come."},
			{Name: "key", Kind: spec.String, Help: "Short key. Defaults to one made from the title."}},
		Effects:  []string{"Adds the recurring item to meeting.md and the ref standing:<key> to the item."},
		Examples: []string{"oj standing from PSEC-6 --place end --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.MakeStanding(ctx.Str("id"), ctx.Str("place"), ctx.Str("key"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "standing", Name: "place",
		Summary: "Put a recurring item at the start or the end of the agendas to come.",
		Params: []spec.Param{{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."},
			{Name: "key", Kind: spec.String, Positional: true, Required: true, Help: "Key of the recurring item."},
			{Name: "place", Kind: spec.String, Positional: true, Required: true, Enum: store.Places, Help: "start or end."}, sphereParam()},
		Effects:  []string{"Rewrites the recurring item's place in meeting.md; instances already written keep theirs."},
		Examples: []string{"oj standing place RDIR repas start --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.SetStandingPlace(ctx.Str("meeting"), ctx.Str("key"), ctx.Str("place"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "standing", Name: "rm",
		Summary:  "Stop a recurring item; the instances already written stay.",
		Params:   []spec.Param{{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."}, {Name: "key", Kind: spec.String, Positional: true, Required: true, Help: "Key of the recurring item."}, sphereParam()},
		Effects:  []string{"Removes the recurring item from meeting.md."},
		Examples: []string{"oj standing rm RDIR repas --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.RemoveStanding(ctx.Str("meeting"), ctx.Str("key"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "standing", Name: "apply",
		Summary: "Write now the instances of a planned sitting's recurring items (all, or one), to act on them by id.",
		Params: []spec.Param{sittingArg(sittingOrAlias), sphereParam(),
			{Name: "key", Kind: spec.String, Help: "Only this recurring item."}},
		Effects:  []string{"Writes the instances as accepted items and places them at the start or the end of the agenda."},
		Examples: []string{"oj standing apply RDIR --sphere pro", "oj standing apply RDIR-2026-10-15 --key repas --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.ApplyStanding(ctx.Str("sitting"), ctx.Str("key"))
		}),
		Text: textItems,
	})
}
