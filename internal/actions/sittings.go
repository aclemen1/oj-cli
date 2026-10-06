package actions

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func sittingArg(help string) spec.Param {
	return spec.Param{Name: "sitting", Kind: spec.String, Positional: true, Required: true, Help: help}
}

const sittingOrAlias = "Sitting id, e.g. RDIR-2026-10-08, or a meeting alias for its next sitting."

func registerSittings() {
	spec.Register(&spec.Action{
		Category: "sitting", Name: "ls", Read: true,
		Summary: "List the sittings of a meeting, or of every meeting of every sphere.",
		Discussion: "Without --since: sittings still open or to come, and the next --ahead dates of each recurrence. " +
			"A date of the recurrence with nothing written yet is listed with virtual: true.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Help: "Meeting alias. Defaults to every meeting."},
			sphereParam(),
			{Name: "state", Kind: spec.String, Default: "all", Enum: append([]string{"all"}, store.SittingStates...), Help: "Keep sittings in this state."},
			{Name: "ahead", Kind: spec.String, Default: "3", Help: "Number of upcoming dates of each recurrence."},
			{Name: "since", Kind: spec.String, Help: "Also list written sittings from this date, YYYY-MM-DD."},
			{Name: "with-items", Kind: spec.Bool, Help: "Return each meeting with its sittings' agendas and its items without a sitting."},
		},
		Examples: []string{"oj sitting ls RDIR --sphere pro", "oj sitting ls --since 2026-01-01 --state minuted --sphere pro",
			"oj sitting ls --with-items --format text --sphere pro"},
		Run: withRead(func(ctx *spec.Context, stores []*store.Store) (any, error) {
			ahead, err := strconv.Atoi(ctx.Str("ahead"))
			if err != nil || ahead < 0 {
				return nil, spec.UserError("--ahead takes a number, got %q. Example: --ahead 5", ctx.Str("ahead"))
			}
			if stores, err = narrow(stores, ctx.Str("meeting")); err != nil {
				return nil, err
			}
			if ctx.Bool("with-items") {
				out := []store.Overview{}
				for _, st := range stores {
					l, err := st.Overviews(ctx.Str("meeting"), ctx.Str("since"), ahead)
					if err != nil {
						return nil, err
					}
					out = append(out, l...)
				}
				return out, nil
			}
			out := []*store.Sitting{}
			for _, st := range stores {
				var ms []*store.Meeting
				if a := ctx.Str("meeting"); a != "" {
					m, err := st.Meeting(a)
					if err != nil {
						return nil, err
					}
					ms = []*store.Meeting{m}
				} else if ms, err = st.Meetings(); err != nil {
					return nil, err
				}
				for _, m := range ms {
					l, err := st.Sittings(m, ctx.Str("since"), ahead, ctx.Str("state"))
					if err != nil {
						return nil, err
					}
					out = append(out, l...)
				}
			}
			return out, nil
		}),
		Text: func(w io.Writer, r any) {
			if ovs, ok := r.([]store.Overview); ok {
				textOverviews(w, ovs)
				return
			}
			l := r.([]*store.Sitting)
			var sph []string
			for _, s := range l {
				sph = append(sph, s.Sphere)
			}
			show := spheresIn(sph...) > 1
			for _, s := range l {
				v := ""
				if s.Virtual {
					v = " (from the recurrence)"
				}
				fmt.Fprintf(w, "%-26s %s %-5s %-9s%s\n", tag(show, s.Sphere)+s.ID, weekday(s.Date), s.Time, s.State, v)
			}
		},
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "show", Read: true,
		Summary:  "Show the agenda of a sitting: ordered items with slots, total time, proposals apart.",
		Params:   []spec.Param{sittingArg(sittingOrAlias), sphereParam()},
		Examples: []string{"oj sitting show RDIR", "oj sitting show pro:RDIR-2026-10-08 --format text"},
		Run: withRead(func(ctx *spec.Context, stores []*store.Store) (any, error) {
			a, _, err := pick(stores, ctx.Str("sitting"), func(st *store.Store) (*store.Agenda, error) { return st.Agenda(ctx.Str("sitting")) })
			return a, err
		}),
		Text: func(w io.Writer, r any) { textAgenda(w, r.(*store.Agenda)) },
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "add",
		Summary: "Add a one-off sitting to a meeting.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."},
			sphereParam(),
			{Name: "date", Kind: spec.String, Required: true, Help: "Date, YYYY-MM-DD."},
			{Name: "time", Kind: spec.String, Help: "Start time, HH:MM."},
			{Name: "place", Kind: spec.String, Help: "Room or link. Defaults to the meeting's."},
		},
		Effects:  []string{"Writes a planned sitting; on a day that already has one, its id takes a suffix (-2)."},
		Examples: []string{"oj sitting add RETRAITE --date 2026-11-20 --time 08:30 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.AddSitting(ctx.Str("meeting"), ctx.Str("date"), ctx.Str("time"), ctx.Str("place"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "move",
		Summary:  "Give a sitting another date or time; its id keeps the original date.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam(), {Name: "date", Kind: spec.String, Help: "New date, YYYY-MM-DD."}, {Name: "time", Kind: spec.String, Help: "New start time, HH:MM."}},
		Effects:  []string{"Rewrites the sitting with its new date; its items stay on it."},
		Examples: []string{"oj sitting move RDIR-2026-10-08 --date 2026-10-09 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.MoveSitting(ctx.Str("sitting"), ctx.Str("date"), ctx.Str("time"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "cancel",
		Summary:  "Cancel a sitting; its items move to the next planned sitting.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam(), {Name: "reason", Kind: spec.String, Help: "Why, for the log."}},
		Effects:  []string{"Marks the sitting cancelled.", "Plans its proposed, accepted and deferred items for the next planned sitting, state unchanged."},
		Examples: []string{`oj sitting cancel RDIR-2026-10-15 --reason "autumn holidays" --sphere pro`},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.CancelSitting(ctx.Str("sitting"), ctx.Str("reason"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "freeze",
		Summary:  "Close the agenda of a sitting.",
		Params:   []spec.Param{sittingArg(sittingOrAlias), sphereParam(), {Name: "leave-proposed", Kind: spec.Bool, Help: "Move items still proposed to the next sitting instead of refusing."}},
		Effects:  []string{"Marks the sitting frozen and records the agenda order.", "With --leave-proposed, plans its proposed items for the next planned sitting."},
		Examples: []string{"oj sitting freeze RDIR --sphere pro", "oj sitting freeze RDIR-2026-10-08 --leave-proposed --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.FreezeSitting(ctx.Str("sitting"), ctx.Bool("leave-proposed"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "reopen",
		Summary:  "Reopen a frozen agenda.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam()},
		Effects:  []string{"Marks the sitting planned again."},
		Examples: []string{"oj sitting reopen RDIR-2026-10-08 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.ReopenSitting(ctx.Str("sitting"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "hold",
		Summary: "Record that a sitting took place, with attendance.",
		Params: []spec.Param{sittingArg(sittingOrAlias), sphereParam(),
			{Name: "present", Kind: spec.StringList, Help: "Who attended (repeatable)."},
			{Name: "excused", Kind: spec.StringList, Help: "Who was excused (repeatable)."}},
		Effects:  []string{"Marks the sitting held and records the agenda order and attendance."},
		Examples: []string{"oj sitting hold RDIR-2026-10-08 --present Marie --present Paul --excused Anne --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.HoldSitting(ctx.Str("sitting"), ctx.List("present"), ctx.List("excused"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "minute",
		Summary: "Approve the minutes of a held sitting.",
		Discussion: "Every outcome of the sitting becomes approved, drafts included. An item whose outcome says next=done becomes done; " +
			"every other item on the agenda is deferred to the next planned sitting. Show the drafts to the user before approving.",
		Params:   []spec.Param{sittingArg("Sitting id."), sphereParam()},
		Effects:  []string{"Marks the sitting minuted.", "Approves its draft outcomes.", "Marks items done or defers them; moves remaining proposals to the next sitting."},
		Examples: []string{"oj sitting minute RDIR-2026-10-08 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.MinuteSitting(ctx.Str("sitting"))
		}),
	})
}

func textAgenda(w io.Writer, a *store.Agenda) {
	s := a.Sitting
	head := strings.TrimSpace(fmt.Sprintf("%s · %s %s", tag(s.Sphere != "", s.Sphere)+s.ID, weekday(s.Date), s.Time))
	fmt.Fprintf(w, "%s\n%s · %s", a.Title, head, s.State)
	if s.Place != "" {
		fmt.Fprintf(w, " · %s", s.Place)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)
	if len(a.Items) == 0 {
		fmt.Fprintln(w, "  (no item on the agenda)")
	}
	for i, ai := range a.Items {
		it := ai.Item
		owner := ""
		if it.Owner != "" {
			owner = " — " + it.Owner
		}
		fmt.Fprintf(w, "%2d. %-6s %-10s %-5s %s%s  [%s, %s]\n", i+1, ai.Start, it.ID, it.Duration, it.Title, owner, it.Kind, it.State)
		if it.Expected != "" {
			fmt.Fprintf(w, "    ? %s\n", it.Expected)
		}
		if o := ai.Outcome; o != nil {
			if o.Summary != "" {
				fmt.Fprintf(w, "    = %s\n", o.Summary)
			}
			if o.Decision != "" {
				fmt.Fprintf(w, "    ! %s\n", o.Decision)
			}
			for _, act := range o.Actions {
				fmt.Fprintf(w, "    > %s (%s, %s)\n", act.What, act.Who, act.Due)
			}
			fmt.Fprintf(w, "    [outcome %s by %s, next %s]\n", o.Status, o.By, o.Next)
		}
	}
	total := "planned " + a.Planned
	if a.Duration != "" {
		total += " of " + a.Duration
	}
	if a.Over {
		total += " — over time"
	}
	fmt.Fprintf(w, "\n%s\n", total)
	if len(a.Proposed) > 0 {
		fmt.Fprintln(w, "\nProposed:")
		for _, it := range a.Proposed {
			fmt.Fprintf(w, "  %-10s %-5s %s\n", it.ID, it.Duration, it.Title)
		}
	}
}

func textOverviews(w io.Writer, ovs []store.Overview) {
	var sph []string
	for _, ov := range ovs {
		sph = append(sph, ov.Sphere)
	}
	show := spheresIn(sph...) > 1
	for i, ov := range ovs {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s · %s\n", tag(show, ov.Sphere)+ov.Meeting, ov.Title)
		for _, a := range ov.Sittings {
			s := a.Sitting
			fmt.Fprintf(w, "\n  %s  %s %s  %s  %d items, %s", s.ID, weekday(s.Date), s.Time, s.State, len(a.Items), a.Planned)
			if a.Duration != "" {
				fmt.Fprintf(w, " of %s", a.Duration)
			}
			fmt.Fprintln(w)
			for _, ai := range a.Items {
				fmt.Fprintf(w, "    %-10s %-9s %s\n", ai.Item.ID, ai.Item.State, ai.Item.Title)
			}
			for _, it := range a.Proposed {
				fmt.Fprintf(w, "    %-10s %-9s %s\n", it.ID, "proposed", it.Title)
			}
		}
		if len(ov.Unplanned) > 0 {
			fmt.Fprintln(w, "\n  no sitting yet")
			for _, it := range ov.Unplanned {
				fmt.Fprintf(w, "    %-10s %-9s %s\n", it.ID, it.State, it.Title)
			}
		}
	}
}

// weekday puts the English weekday before a YYYY-MM-DD date.
func weekday(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.Format("Mon") + " " + d
}
