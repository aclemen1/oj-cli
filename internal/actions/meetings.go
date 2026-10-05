package actions

import (
	"fmt"
	"io"

	"github.com/aclemen1/ordo-cli/internal/spec"
	"github.com/aclemen1/ordo-cli/internal/store"
)

type meetingSummary struct {
	Alias    string `json:"alias"`
	Title    string `json:"title"`
	Next     string `json:"next,omitempty"`
	NextDate string `json:"next_date,omitempty"`
	Accepted int    `json:"accepted"`
	Proposed int    `json:"proposed"`
}

func meetingInput(ctx *spec.Context) store.MeetingInput {
	return store.MeetingInput{
		Title: ctx.Str("title"), RRule: ctx.Str("rrule"), Start: ctx.Str("start"), TZ: ctx.Str("tz"),
		Duration: ctx.Str("duration"), Place: ctx.Str("place"), Chair: ctx.Str("chair"),
		ItemDuration: ctx.Str("item-duration"), ItemKind: ctx.Str("item-kind"),
		Members: ctx.List("member"), Refs: ctx.List("ref"),
	}
}

func meetingFields() []spec.Param {
	return []spec.Param{
		{Name: "title", Kind: spec.String, Help: "Name of the meeting, e.g. \"Séance de direction\"."},
		{Name: "rrule", Kind: spec.String, Help: "iCalendar recurrence, e.g. FREQ=WEEKLY;BYDAY=TH or FREQ=MONTHLY;BYDAY=1TU. Needs --start."},
		{Name: "start", Kind: spec.String, Help: "First sitting of the recurrence, YYYY-MM-DDTHH:MM."},
		{Name: "tz", Kind: spec.String, Help: "IANA time zone of the recurrence. Defaults to this machine's."},
		{Name: "duration", Kind: spec.String, Help: "Length of a sitting, e.g. 90m or 2h."},
		{Name: "place", Kind: spec.String, Help: "Usual room or link."},
		{Name: "chair", Kind: spec.String, Help: "Who chairs."},
		{Name: "member", Kind: spec.StringList, Help: "Member (repeatable); on edit, replaces the list."},
		{Name: "ref", Kind: spec.StringList, Help: "Link to another tool (repeatable), e.g. office:U-RDIR; on edit, replaces the list."},
		{Name: "item-duration", Kind: spec.String, Help: "Default duration of an item, e.g. 10m."},
		{Name: "item-kind", Kind: spec.String, Enum: store.Kinds, Help: "Default kind of an item."},
	}
}

func registerMeetings() {
	spec.Register(&spec.Action{
		Category: "meeting", Name: "add",
		Summary: "Create a meeting, recurring or one-off.",
		Discussion: "A recurring meeting has --rrule and --start; its sittings follow the recurrence. " +
			"A one-off meeting has neither; add its sittings with `ordo sitting add`.",
		Params: append([]spec.Param{
			{Name: "alias", Kind: spec.String, Positional: true, Required: true, Help: "Short upper-case name, unique in the sphere, e.g. RDIR."},
			sphereParam(),
		}, meetingFields()...),
		Effects: []string{"Creates the meeting's directory and meeting.md in the sphere's store."},
		Examples: []string{
			`ordo meeting add RDIR --title "Séance de direction" --rrule "FREQ=WEEKLY;BYDAY=TH" --start 2026-10-08T09:00 --duration 90m --sphere pro`,
			`ordo meeting add RETRAITE --title "Retraite annuelle" --sphere pro`,
		},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.AddMeeting(ctx.Str("alias"), meetingInput(ctx))
		}),
	})
	spec.Register(&spec.Action{
		Category: "meeting", Name: "edit",
		Summary:  "Change fields of a meeting.",
		Params:   append([]spec.Param{{Name: "alias", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."}, sphereParam()}, meetingFields()...),
		Effects:  []string{"Rewrites meeting.md and logs the fields changed. Sittings already written keep their date."},
		Examples: []string{"ordo meeting edit RDIR --duration 2h --sphere pro", `ordo meeting edit RDIR --rrule "FREQ=WEEKLY;BYDAY=WE" --sphere pro`},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.EditMeeting(ctx.Str("alias"), meetingInput(ctx))
		}),
	})
	spec.Register(&spec.Action{
		Category: "meeting", Name: "show",
		Summary:  "Show a meeting: recurrence, members, defaults, log.",
		Params:   []spec.Param{{Name: "alias", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."}, sphereParam()},
		Examples: []string{"ordo meeting show RDIR --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			return st.Meeting(ctx.Str("alias"))
		}),
	})
	spec.Register(&spec.Action{
		Category: "meeting", Name: "ls",
		Summary:  "List the meetings of a sphere with their next sitting.",
		Params:   []spec.Param{sphereParam()},
		Examples: []string{"ordo meeting ls --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			ms, err := st.Meetings()
			if err != nil {
				return nil, err
			}
			out := []meetingSummary{}
			for _, m := range ms {
				sum := meetingSummary{Alias: m.Alias, Title: m.Title}
				if a, err := st.Agenda(m.Alias); err == nil {
					sum.Next, sum.NextDate = a.Sitting.ID, a.Sitting.Date
					sum.Accepted, sum.Proposed = len(a.Items), len(a.Proposed)
				}
				out = append(out, sum)
			}
			return out, nil
		}),
		Text: func(w io.Writer, r any) {
			for _, m := range r.([]meetingSummary) {
				next := "no upcoming sitting"
				if m.Next != "" {
					next = fmt.Sprintf("next %s: %d on the agenda, %d proposed", m.Next, m.Accepted, m.Proposed)
				}
				fmt.Fprintf(w, "%-10s %-40s %s\n", m.Alias, m.Title, next)
			}
		},
	})
}
