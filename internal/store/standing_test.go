package store

import (
	"strings"
	"testing"
)

func agendaTitles(a *Agenda) string {
	var l []string
	for _, ai := range a.Items {
		mark := ""
		if ai.Item.Virtual {
			mark = "~"
		}
		l = append(l, mark+ai.Item.Title)
	}
	return strings.Join(l, " | ")
}

func TestStandingItems(t *testing.T) {
	s := withRDIR(t)
	must[*Meeting](t)(s.AddStanding("RDIR", Standing{Title: "Logistique du déplacement", Place: "start", Duration: "5m"}))
	m := must[*Meeting](t)(s.AddStanding("RDIR", Standing{Title: "Date de la prochaine séance"}))
	if !strings.HasPrefix(m.Standing[0].Key, "logistique-du-deplac") || m.Standing[1].Place != "end" {
		t.Fatalf("declared %+v", m.Standing)
	}
	if _, err := s.AddStanding("RDIR", Standing{Title: "Date de la prochaine séance"}); kind(err) != "conflict" {
		t.Fatal("same key twice")
	}
	key := m.Standing[1].Key
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, ""))

	// Every sitting shows them, virtual, in place; nothing is written.
	a := must[*Agenda](t)(s.Agenda("RDIR"))
	if got := agendaTitles(a); got != "~Logistique du déplacement | Budget | ~Date de la prochaine séance" {
		t.Fatalf("agenda: %s", got)
	}
	if a.Items[0].Start != "09:00" || a.Items[1].Start != "09:05" {
		t.Fatalf("slots %s %s", a.Items[0].Start, a.Items[1].Start)
	}
	later := must[*Agenda](t)(s.Agenda("RDIR-2026-10-22"))
	if got := agendaTitles(later); got != "~Logistique du déplacement | ~Date de la prochaine séance" {
		t.Fatalf("later agenda: %s", got)
	}
	if n := len(must[[]*Item](t)(s.Items(ItemFilter{Meeting: "RDIR", State: "all"}))); n != 1 {
		t.Fatalf("virtual items were written: %d items", n)
	}

	// Applying one writes it, accepted, at the end; applying again returns it.
	made := must[[]*Item](t)(s.ApplyStanding("RDIR", key))
	if len(made) != 1 || made[0].State != "accepted" || made[0].StandingKey() != key {
		t.Fatalf("apply %+v", made)
	}
	again := must[[]*Item](t)(s.ApplyStanding("RDIR", key))
	if len(again) != 1 || again[0].ID != made[0].ID {
		t.Fatalf("apply again %+v", again)
	}
	a = must[*Agenda](t)(s.Agenda("RDIR"))
	if got := agendaTitles(a); got != "~Logistique du déplacement | Budget | Date de la prochaine séance" {
		t.Fatalf("after apply: %s", got)
	}

	// Freezing writes the rest, in place.
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	a = must[*Agenda](t)(s.Agenda("RDIR-2026-10-08"))
	if got := agendaTitles(a); got != "Logistique du déplacement | Budget | Date de la prochaine séance" {
		t.Fatalf("frozen: %s", got)
	}

	// At the minutes, a recurring item not reached is dropped, not deferred.
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", nil, nil))
	logi := a.Items[0].Item.ID
	must[*Item](t)(s.SetOutcome(logi, "", OutcomeInput{Summary: "Train de 7h"}))
	mn := must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	if strings.Join(mn.Done, ",") != logi || strings.Join(mn.Deferred, ",") != "RDIR-1" || strings.Join(mn.Dropped, ",") != made[0].ID {
		t.Fatalf("minuted %+v", mn)
	}
	// The next sitting has its own instances, and the deferred item.
	next := must[*Agenda](t)(s.Agenda("RDIR-2026-10-15"))
	if got := agendaTitles(next); got != "~Logistique du déplacement | Budget | ~Date de la prochaine séance" {
		t.Fatalf("next sitting: %s", got)
	}

	// A drop holds for its sitting; removing the declaration stops new ones.
	must[[]*Item](t)(s.ApplyStanding("RDIR-2026-10-22", key))
	inst := must[[]*Item](t)(s.Items(ItemFilter{Meeting: "RDIR", Sitting: "RDIR-2026-10-22"}))[0]
	must[*Item](t)(s.DropItem(inst.ID, "pas cette fois"))
	if got := agendaTitles(must[*Agenda](t)(s.Agenda("RDIR-2026-10-22"))); got != "~Logistique du déplacement" {
		t.Fatalf("after a drop: %s", got)
	}
	must[*Meeting](t)(s.RemoveStanding("RDIR", key))
	if got := agendaTitles(must[*Agenda](t)(s.Agenda("RDIR-2026-10-29"))); got != "~Logistique du déplacement" {
		t.Fatalf("after rm: %s", got)
	}
}
