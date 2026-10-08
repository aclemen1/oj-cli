package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aclemen1/oj-cli/internal/spec"
)

// today is Monday 5 October 2026; RDIR meets on Thursdays at 09:00.
var today = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

func newStore(t *testing.T, vcs string) *Store {
	t.Helper()
	root := t.TempDir()
	if err := Init(root, vcs); err != nil {
		t.Fatal(err)
	}
	return &Store{Sphere: "pro", Root: root, VCS: vcs, By: "alain", Now: func() time.Time { return today },
		Warn: func(m string) { t.Errorf("warning: %s", m) }}
}

func withRDIR(t *testing.T) *Store {
	t.Helper()
	s := newStore(t, "none")
	_, err := s.AddMeeting("rdir", MeetingInput{Title: "Séance de direction", RRule: "FREQ=WEEKLY;BYDAY=TH",
		Start: "2026-10-01T09:00", TZ: "Europe/Zurich", Duration: "90m", ItemDuration: "10m"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func kind(err error) string {
	var e *spec.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}

func TestDurationsAndRRule(t *testing.T) {
	for in, want := range map[string]string{"15m": "15m", "90": "1h30m", "90m": "1h30m", "2h": "2h", "1h05m": "1h5m"} {
		if got, err := NormDuration(in); err != nil || got != want {
			t.Errorf("NormDuration(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormDuration("10s"); err == nil {
		t.Error("10s should be refused")
	}
	r, err := BuildRRule("FREQ=MONTHLY;BYDAY=1TU", "2026-11-03T14:00", "Europe/Zurich")
	if err != nil || r != "DTSTART;TZID=Europe/Zurich:20261103T140000\nRRULE:FREQ=MONTHLY;BYDAY=1TU" {
		t.Fatalf("BuildRRule = %q, %v", r, err)
	}
	if _, err := BuildRRule("FREQ=WEEKLY", "", ""); err == nil {
		t.Error("a rule without start should be refused")
	}
}

func TestNextSittingFromRecurrence(t *testing.T) {
	s := withRDIR(t)
	a := must[*Agenda](t)(s.Agenda("RDIR"))
	if a.Sitting.ID != "RDIR-2026-10-08" || !a.Sitting.Virtual || a.Sitting.Time != "09:00" {
		t.Fatalf("next sitting %+v", a.Sitting)
	}
	l := must[[]*Sitting](t)(s.Sittings(must[*Meeting](t)(s.Meeting("RDIR")), "", 3, ""))
	if len(l) != 3 || l[2].ID != "RDIR-2026-10-22" {
		t.Fatalf("sittings %v", sittingIDs(l))
	}
	if _, _, err := s.Sitting("RDIR-2026-10-09"); kind(err) != "not_found" {
		t.Fatalf("a Friday is no sitting: %v", err)
	}
}

func sittingIDs(l []*Sitting) []string {
	var out []string
	for _, x := range l {
		out = append(out, x.ID)
	}
	return out
}

func TestAgendaOrderAndSlots(t *testing.T) {
	s := withRDIR(t)
	it1 := must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget 2027", Kind: "decision", Duration: "20m"}, true, ""))
	it2 := must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Point RH"}, true, ""))
	it3 := must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Audit S3", Refs: []string{"office:U-0042"}}, false, ""))
	if it1.ID != "RDIR-1" || it2.Duration != "10m" || it2.Kind != "discussion" || it3.State != "proposed" || it3.Sitting != "RDIR-2026-10-08" {
		t.Fatalf("items %+v %+v %+v", it1, it2, it3)
	}
	a := must[*Agenda](t)(s.OrderItems("RDIR", []string{"RDIR-2"}))
	if len(a.Items) != 2 || a.Items[0].Item.ID != "RDIR-2" || a.Items[1].Start != "09:10" || a.Planned != "30m" || len(a.Proposed) != 1 {
		t.Fatalf("agenda %+v", a)
	}
	if a.Sitting.Virtual {
		t.Fatal("ordering should write the sitting")
	}
	if _, err := s.OrderItems("RDIR", []string{"RDIR-3"}); kind(err) != "user_error" {
		t.Fatalf("a proposal cannot be ordered: %v", err)
	}
	found := must[[]*Item](t)(s.Items(ItemFilter{Ref: "office:U-0042"}))
	if len(found) != 1 || found[0].ID != "RDIR-3" {
		t.Fatalf("by ref %v", found)
	}
}

func TestFreezeRefusesProposals(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "B"}, false, ""))
	if _, err := s.FreezeSitting("RDIR", false); kind(err) != "conflict" {
		t.Fatalf("freeze with a proposal: %v", err)
	}
	ch := must[*Changed](t)(s.FreezeSitting("RDIR", true))
	if ch.Sitting.State != "frozen" || len(ch.Moved) != 1 {
		t.Fatalf("freeze %+v", ch)
	}
	b := must[*Item](t)(s.Item("RDIR-2"))
	if b.Sitting != "RDIR-2026-10-15" || b.State != "proposed" {
		t.Fatalf("moved proposal %+v", b)
	}
	// A new item goes past the frozen sitting, and nothing is added to it.
	c := must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "C"}, true, ""))
	if c.Sitting != "RDIR-2026-10-15" {
		t.Fatalf("new item on %s", c.Sitting)
	}
	if _, err := s.AddItem("RDIR", ItemInput{Title: "D"}, true, "RDIR-2026-10-08"); kind(err) != "conflict" {
		t.Fatalf("adding to a frozen sitting: %v", err)
	}
	must[*Sitting](t)(s.ReopenSitting("RDIR-2026-10-08"))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "D"}, true, "RDIR-2026-10-08"))
}

func TestFullCycle(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget", Kind: "decision"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "RH"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Pas abordé"}, true, ""))
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	if _, err := s.MinuteSitting("RDIR-2026-10-08"); kind(err) != "conflict" {
		t.Fatalf("minute before hold: %v", err)
	}
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", []string{"Marie", "Paul"}, nil))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "Approuvé", Actions: []string{"Envoyer au rectorat|Marie|2026-10-20"}}))
	o := must[*Item](t)(s.SetOutcome("RDIR-2", "", OutcomeInput{Summary: "Discuté", By: "agent:claude"}))
	if o.History[0].Outcome.Status != "draft" {
		t.Fatalf("agent outcome %+v", o.History[0].Outcome)
	}
	if _, err := s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "x", Actions: []string{"|bad"}}); kind(err) != "user_error" {
		t.Fatalf("bad action: %v", err)
	}
	m := must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	if strings.Join(m.Done, ",") != "RDIR-1,RDIR-2" || strings.Join(m.Deferred, ",") != "RDIR-3" || m.Approved != 1 {
		t.Fatalf("minuted %+v", m)
	}
	it3 := must[*Item](t)(s.Item("RDIR-3"))
	if it3.State != "deferred" || it3.Sitting != "RDIR-2026-10-15" || it3.History[0].Result != "deferred" {
		t.Fatalf("deferred item %+v", it3)
	}
	it1 := must[*Item](t)(s.Item("RDIR-1"))
	if it1.State != "done" || it1.History[0].Outcome.Actions[0].Due != "2026-10-20" {
		t.Fatalf("done item %+v", it1)
	}
	// The minuted agenda still lists the deferred item with its result.
	a := must[*Agenda](t)(s.Agenda("RDIR-2026-10-08"))
	if len(a.Items) != 3 {
		t.Fatalf("minuted agenda %d items", len(a.Items))
	}
	if next := must[*Agenda](t)(s.Agenda("RDIR")); next.Sitting.ID != "RDIR-2026-10-15" || next.Items[0].Item.ID != "RDIR-3" {
		t.Fatalf("next agenda %+v", next)
	}
	// An outcome on a minuted sitting is refused.
	if _, err := s.SetOutcome("RDIR-1", "RDIR-2026-10-08", OutcomeInput{Summary: "late"}); kind(err) != "conflict" {
		t.Fatalf("outcome after minutes: %v", err)
	}
}

func TestCancelMoveDeferDrop(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "B"}, false, ""))
	ch := must[*Changed](t)(s.CancelSitting("RDIR-2026-10-08", "vacances"))
	if ch.Sitting.State != "cancelled" || len(ch.Moved) != 2 {
		t.Fatalf("cancel %+v", ch)
	}
	if a := must[*Agenda](t)(s.Agenda("RDIR")); a.Sitting.ID != "RDIR-2026-10-15" || len(a.Items) != 1 || len(a.Proposed) != 1 {
		t.Fatalf("after cancel %+v", a)
	}
	moved := must[*Sitting](t)(s.MoveSitting("RDIR-2026-10-15", "2026-10-16", "14:00"))
	if moved.ID != "RDIR-2026-10-15" || moved.Date != "2026-10-16" || moved.Time != "14:00" {
		t.Fatalf("moved %+v", moved)
	}
	d := must[*Item](t)(s.DeferItem("RDIR-1", ""))
	if d.Sitting != "RDIR-2026-10-22" || d.State != "deferred" {
		t.Fatalf("defer %+v", d)
	}
	if _, err := s.DropItem("RDIR-2", ""); kind(err) != "user_error" {
		t.Fatal("drop needs a reason")
	}
	must[*Item](t)(s.DropItem("RDIR-2", "réglé par e-mail"))
	if _, err := s.AcceptItems([]string{"RDIR-2"}); kind(err) != "conflict" {
		t.Fatalf("accept a dropped item: %v", err)
	}
	if open := must[[]*Item](t)(s.Items(ItemFilter{Meeting: "RDIR"})); len(open) != 1 {
		t.Fatalf("open items %d", len(open))
	}
}

func TestOneOffMeeting(t *testing.T) {
	s := newStore(t, "none")
	must[*Meeting](t)(s.AddMeeting("RETRAITE", MeetingInput{Title: "Retraite"}))
	it := must[*Item](t)(s.AddItem("RETRAITE", ItemInput{Title: "Stratégie"}, true, ""))
	if it.Sitting != "" {
		t.Fatalf("no sitting yet, got %s", it.Sitting)
	}
	if _, err := s.Agenda("RETRAITE"); kind(err) != "not_found" {
		t.Fatalf("agenda without sitting: %v", err)
	}
	a := must[*Sitting](t)(s.AddSitting("RETRAITE", "2026-11-20", "08:30", ""))
	b := must[*Sitting](t)(s.AddSitting("RETRAITE", "2026-11-20", "14:00", ""))
	if a.ID != "RETRAITE-2026-11-20" || b.ID != "RETRAITE-2026-11-20-2" {
		t.Fatalf("one-off ids %s %s", a.ID, b.ID)
	}
	if got := must[[]*Item](t)(s.AcceptItems([]string{it.ID})); got[0].Sitting != "" {
		t.Fatal("accepting an accepted item changes nothing")
	}
	d := must[*Item](t)(s.DeferItem(it.ID, ""))
	if d.Sitting != "RETRAITE-2026-11-20" {
		t.Fatalf("an item with no sitting goes to the first one: %s", d.Sitting)
	}
	if _, err := s.AddMeeting("RETRAITE", MeetingInput{Title: "x"}); kind(err) != "conflict" {
		t.Fatalf("duplicate alias: %v", err)
	}
}

func TestMeetingEditKeepsStart(t *testing.T) {
	s := withRDIR(t)
	m := must[*Meeting](t)(s.EditMeeting("RDIR", MeetingInput{RRule: "FREQ=WEEKLY;BYDAY=WE"}))
	if !strings.Contains(m.RRule, "20261001T090000") || !strings.HasSuffix(m.RRule, "BYDAY=WE") {
		t.Fatalf("rrule %q", m.RRule)
	}
	if a := must[*Agenda](t)(s.Agenda("RDIR")); a.Sitting.ID != "RDIR-2026-10-07" {
		t.Fatalf("next after edit %s", a.Sitting.ID)
	}
	if _, err := s.EditMeeting("RDIR", MeetingInput{}); kind(err) != "user_error" {
		t.Fatalf("empty edit: %v", err)
	}
}

func TestHandEditedFileIsRead(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	p := filepath.Join(s.Root, "RDIR", "items", "RDIR-1.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "title: A", "title: Corrigé à la main", 1)+"\nNotes libres.\n"), 0o644)
	it := must[*Item](t)(s.Item("RDIR-1"))
	if it.Title != "Corrigé à la main" || it.Notes != "Notes libres." {
		t.Fatalf("hand edit %+v", it)
	}
}

func TestActionDoneWhenWritten(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, ""))
	it := must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "ok",
		Actions: []string{"Réserver la salle|Paul||fait", "Envoyer le PV|Marie|2026-10-20"}}))
	a := it.History[0].Outcome.Actions
	if !a[0].Done || a[1].Done || a[0].Due != "" {
		t.Fatalf("actions %+v", a)
	}
	if open := must[[]ActionRow](t)(s.Actions(ActionFilter{})); len(open) != 1 || open[0].What != "Envoyer le PV" {
		t.Fatalf("open actions %+v", open)
	}
	// Rewriting without the mark keeps it done.
	it = must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "ok", Actions: []string{"Réserver la salle|Paul|", "Envoyer le PV|Marie|2026-10-20|done"}}))
	if a = it.History[0].Outcome.Actions; !a[0].Done || !a[1].Done {
		t.Fatalf("after rewrite %+v", a)
	}
	if _, err := ParseAction("x|y|2026-10-20|peut-être"); kind(err) != "user_error" {
		t.Fatal("an unknown mark is refused")
	}
}

func TestActions(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, ""))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "Approuvé",
		Actions: []string{"Envoyer|Marie|2026-10-20", "Informer l'équipe|Paul|", "Archiver||2026-10-09"}}))
	l := must[[]ActionRow](t)(s.Actions(ActionFilter{}))
	if len(l) != 3 || l[0].What != "Archiver" || l[2].Due != "" {
		t.Fatalf("order by due: %+v", l)
	}
	if mine := must[[]ActionRow](t)(s.Actions(ActionFilter{Who: "marie"})); len(mine) != 1 || mine[0].N != 1 {
		t.Fatalf("by who: %+v", mine)
	}
	must[*Item](t)(s.SetActionDone("RDIR-1", "", 1, true))
	if open := must[[]ActionRow](t)(s.Actions(ActionFilter{})); len(open) != 2 {
		t.Fatalf("open after done: %d", len(open))
	}
	// Rewriting the outcome keeps what is done.
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "Approuvé tel quel",
		Actions: []string{"Envoyer|Marie|2026-10-20", "Informer l'équipe|Paul|"}}))
	if done := must[[]ActionRow](t)(s.Actions(ActionFilter{State: "done"})); len(done) != 1 || done[0].What != "Envoyer" {
		t.Fatalf("done kept: %+v", done)
	}
	if _, err := s.SetActionDone("RDIR-1", "", 9, true); kind(err) != "not_found" {
		t.Fatalf("missing action: %v", err)
	}
}

func TestImportTasks(t *testing.T) {
	s := withRDIR(t)
	raw := []byte(`{"items": [
	  {"id": "t1", "title": "Audit S3", "notes": "voir le rapport", "status": "needsAction",
	   "links": [{"type": "email", "link": "https://mail.google.com/mail/#all/18f0abc"}]},
	  {"id": "t2", "title": "Ancien point", "status": "completed"},
	  {"id": "t3", "title": "Budget", "status": "needsAction", "due": "2026-10-20T00:00:00.000Z"},
	  {"id": "t4", "title": "  ", "status": "needsAction"}
	]}`)
	tasks := must[[]Task](t)(ParseTasks(raw))
	dry := must[*Imported](t)(s.ImportTasks("RDIR", tasks, false, false, true))
	if len(dry.Created) != 2 || dry.Created[0].ID != "" {
		t.Fatalf("dry run %+v", dry)
	}
	if l := must[[]*Item](t)(s.Items(ItemFilter{Meeting: "RDIR", State: "all"})); len(l) != 0 {
		t.Fatal("dry run wrote items")
	}
	got := must[*Imported](t)(s.ImportTasks("RDIR", tasks, false, false, false))
	if len(got.Created) != 2 || len(got.Skipped) != 2 {
		t.Fatalf("import %+v", got)
	}
	a := got.Created[0]
	if a.ID != "RDIR-1" || a.State != "proposed" || a.Sitting != "RDIR-2026-10-08" ||
		strings.Join(a.Refs, " ") != "gtasks:t1 gmail:message/18f0abc" || a.Notes != "voir le rapport" {
		t.Fatalf("first item %+v", a)
	}
	if !strings.Contains(got.Created[1].Notes, "2026-10-20") {
		t.Fatalf("due date lost: %q", got.Created[1].Notes)
	}
	again := must[*Imported](t)(s.ImportTasks("RDIR", tasks, false, false, false))
	if len(again.Created) != 0 || again.Skipped[0]["reason"] != "already imported" || again.Skipped[2]["reason"] != "already imported" {
		t.Fatalf("replay %+v", again)
	}
	if _, err := ParseTasks([]byte(`nope`)); kind(err) != "user_error" {
		t.Fatal("bad JSON should be a user error")
	}
}

func TestConcurrentAddsGetDistinctIDs(t *testing.T) {
	s := withRDIR(t)
	const n = 12
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			it, err := s.AddItem("RDIR", ItemInput{Title: "x"}, false, "")
			if err != nil {
				errs <- err
				return
			}
			ids <- it.ID
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatal(err)
		case id := <-ids:
			if seen[id] {
				t.Fatalf("id %s given twice", id)
			}
			seen[id] = true
		}
	}
	if m := must[*Meeting](t)(s.Meeting("RDIR")); m.Counter != n {
		t.Fatalf("counter %d", m.Counter)
	}
}

func TestJJCommitPerAction(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	s := newStore(t, "jj")
	must[*Meeting](t)(s.AddMeeting("RDIR", MeetingInput{Title: "Séance"}))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, false, ""))
	out, err := runIn(s.Root, "jj", "log", "--no-graph", "-r", "::@-", "-T", `description.first_line() ++ "\n"`)
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "item add RDIR-1") || !strings.Contains(out, "meeting add RDIR") {
		t.Fatalf("jj log:\n%s", out)
	}
}
