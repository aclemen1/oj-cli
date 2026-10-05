package store

import (
	"os"
	"strings"
	"testing"
)

func TestUndoSitting(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "B"}, true, ""))
	// Held without freezing: unhold goes back to planned.
	must[*Sitting](t)(s.HoldSitting("RDIR", nil, nil))
	if sit := must[*Sitting](t)(s.UnholdSitting("RDIR-2026-10-08")); sit.State != "planned" {
		t.Fatalf("unhold of a never frozen sitting: %s", sit.State)
	}
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", nil, nil))
	if sit := must[*Sitting](t)(s.UnholdSitting("RDIR-2026-10-08")); sit.State != "frozen" {
		t.Fatalf("unhold: %s", sit.State)
	}
	if _, err := s.UnholdSitting("RDIR-2026-10-08"); kind(err) != "conflict" {
		t.Fatal("unhold needs a held sitting")
	}
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", nil, nil))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "OK", By: "agent:claude"}))
	mn := must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	if _, err := os.Stat(mn.Rendered); err != nil {
		t.Fatal(err)
	}
	un := must[*Unminuted](t)(s.UnminuteSitting("RDIR-2026-10-08"))
	if un.Sitting.State != "held" || strings.Join(un.Reopened, ",") != "RDIR-1,RDIR-2" || un.Drafts != 1 || len(un.Removed) != 1 {
		t.Fatalf("unminute %+v", un)
	}
	if _, err := os.Stat(mn.Rendered); !os.IsNotExist(err) {
		t.Fatal("the rendered minutes should be removed")
	}
	a, b := must[*Item](t)(s.Item("RDIR-1")), must[*Item](t)(s.Item("RDIR-2"))
	if a.State != "accepted" || a.History[0].Result != "" || a.History[0].Outcome.Status != "draft" {
		t.Fatalf("done item back %+v", a)
	}
	if b.State != "accepted" || b.Sitting != "RDIR-2026-10-08" || b.DeferredFrom != "" {
		t.Fatalf("deferred item back %+v", b)
	}
	// Minute again, then change an item: unminute refuses.
	must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	must[*Item](t)(s.DeferItem("RDIR-2", "RDIR-2026-10-22"))
	if _, err := s.UnminuteSitting("RDIR-2026-10-08"); kind(err) != "conflict" || !strings.Contains(err.Error(), "RDIR-2") {
		t.Fatalf("unminute after a change: %v", err)
	}
	// Cancel and restore.
	must[*Changed](t)(s.CancelSitting("RDIR-2026-10-15", "vacances"))
	if sit := must[*Sitting](t)(s.RestoreSitting("RDIR-2026-10-15")); sit.State != "planned" || sit.Reason != "" {
		t.Fatalf("restore %+v", sit)
	}
}

func TestUndoItem(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	// Undefer before the sitting: back where it was.
	must[*Item](t)(s.DeferItem("RDIR-1", ""))
	it := must[*Item](t)(s.UndeferItem("RDIR-1"))
	if it.State != "accepted" || it.Sitting != "RDIR-2026-10-08" || it.DeferredFrom != "" {
		t.Fatalf("undefer %+v", it)
	}
	if _, err := s.UndeferItem("RDIR-1"); kind(err) != "conflict" {
		t.Fatal("undefer needs a deferred item")
	}
	// Drop and restore.
	must[*Item](t)(s.DropItem("RDIR-1", "doublon"))
	if a := must[*Agenda](t)(s.Agenda("RDIR")); len(a.Dropped) != 1 || len(a.Items) != 0 {
		t.Fatalf("dropped list %+v", a)
	}
	it = must[*Item](t)(s.RestoreItem("RDIR-1", false))
	if it.State != "proposed" || it.Reason != "" || it.Sitting != "RDIR-2026-10-08" {
		t.Fatalf("restore dropped %+v", it)
	}
	must[*Item](t)(s.DropItem("RDIR-1", "doublon"))
	if it = must[*Item](t)(s.RestoreItem("RDIR-1", true)); it.State != "accepted" {
		t.Fatalf("restore --accept %+v", it)
	}
	// Done, then restored: on the next planned sitting.
	must[*Sitting](t)(s.HoldSitting("RDIR", nil, nil))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "fait"}))
	must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	it = must[*Item](t)(s.RestoreItem("RDIR-1", false))
	if it.State != "accepted" || it.Sitting != "RDIR-2026-10-15" || it.History[0].Result != "done" {
		t.Fatalf("restore done %+v", it)
	}
	// Deferred by the minutes of a minuted sitting: undefer only accepts it where it is.
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "B"}, true, ""))
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-15", nil, nil))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "encore"}))
	must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-15"))
	b := must[*Item](t)(s.Item("RDIR-2"))
	if b.State != "deferred" || b.DeferredFrom != "RDIR-2026-10-15" {
		t.Fatalf("deferred by minutes %+v", b)
	}
	if b = must[*Item](t)(s.UndeferItem("RDIR-2")); b.State != "accepted" || b.Sitting != "RDIR-2026-10-22" {
		t.Fatalf("undefer after minutes %+v", b)
	}
}

func TestDeferredListAndOverviews(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	must[*Item](t)(s.DeferItem("RDIR-1", ""))
	a := must[*Agenda](t)(s.Agenda("RDIR-2026-10-08"))
	if len(a.Deferred) != 1 || a.Deferred[0].ID != "RDIR-1" || len(a.Items) != 0 {
		t.Fatalf("deferred list %+v", a)
	}
	must[*Meeting](t)(s.AddMeeting("ONEOFF", MeetingInput{Title: "Ponctuelle"}))
	must[*Item](t)(s.AddItem("ONEOFF", ItemInput{Title: "En attente"}, false, ""))
	ovs := must[[]Overview](t)(s.Overviews("", "", 3))
	if len(ovs) != 2 || ovs[0].Meeting != "ONEOFF" || len(ovs[0].Unplanned) != 1 || len(ovs[1].Sittings) != 3 {
		t.Fatalf("overviews %+v", ovs)
	}
	if next := ovs[1].Sittings[1]; next.Sitting.ID != "RDIR-2026-10-15" || len(next.Items) != 1 {
		t.Fatalf("second sitting %+v", next)
	}
}
