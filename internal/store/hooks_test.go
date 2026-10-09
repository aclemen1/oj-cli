package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/aclemen1/oj-cli/internal/config"
)

// TestMain lets the test binary act as a hook: it records the event and
// whether the store's lock is free while the hook runs.
func TestMain(m *testing.M) {
	if out := os.Getenv("OJ_TEST_HOOK_OUT"); out != "" {
		var ev map[string]any
		json.NewDecoder(os.Stdin).Decode(&ev)
		lock := "free"
		if f, err := os.OpenFile(filepath.Join(os.Getenv("OJ_TEST_ROOT"), ".oj", "lock"), os.O_RDWR, 0); err == nil {
			if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
				lock = "held"
			}
			f.Close()
		}
		ev["lock"] = lock
		ev["depth"] = os.Getenv("OJ_HOOK_DEPTH")
		b, _ := json.Marshal(ev)
		f, _ := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		f.Write(append(b, '\n'))
		f.Close()
		if os.Getenv("OJ_EVENT") == "item.dropped" {
			os.Stderr.WriteString("refused")
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func recorded(t *testing.T, p string) []map[string]any {
	t.Helper()
	b, _ := os.ReadFile(p)
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func TestHooks(t *testing.T) {
	s := withRDIR(t)
	must[*Meeting](t)(s.AddMeeting("OPS", MeetingInput{Title: "Autre"}))
	out := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("OJ_TEST_HOOK_OUT", "")
	exe, _ := os.Executable()
	var warns []string
	s.Warn = func(m string) { warns = append(warns, m) }
	s.Hooks = []config.Hook{
		{On: []string{"*"}, Meetings: []string{"RDIR"}, Run: []string{"env", "OJ_TEST_HOOK_OUT=" + out, "OJ_TEST_ROOT=" + s.Root, exe}},
	}
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, false, ""))
	must[*Item](t)(s.AddItem("OPS", ItemInput{Title: "Ailleurs"}, false, ""))
	must[[]*Item](t)(s.AcceptItems([]string{"RDIR-1"}))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "OK", Actions: []string{"Envoyer|Marie|"}}))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Doublon"}, false, ""))
	must[*Item](t)(s.DropItem("RDIR-2", "doublon"))

	evs := recorded(t, out)
	var names []string
	for _, e := range evs {
		names = append(names, e["event"].(string))
		if e["lock"] != "free" {
			t.Fatalf("hook %s ran under the lock", e["event"])
		}
		if e["depth"] != "1" || e["sphere"] != "pro" || e["meeting"] != "RDIR" {
			t.Fatalf("hook context %v", e)
		}
	}
	want := "item.added item.accepted outcome.set action.added item.added item.dropped"
	if strings.Join(names, " ") != want {
		t.Fatalf("events %q, want %q", strings.Join(names, " "), want)
	}
	if it := evs[2]["item"].(map[string]any); it["id"] != "RDIR-1" {
		t.Fatalf("payload %v", evs[2])
	}
	if a := evs[3]["action"].(map[string]any); a["what"] != "Envoyer" {
		t.Fatalf("action payload %v", evs[3])
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "item.dropped") || !strings.Contains(warns[0], "refused") {
		t.Fatalf("warnings %v", warns)
	}
	if it := must[*Item](t)(s.Item("RDIR-2")); it.State != "dropped" {
		t.Fatal("a failing hook must not undo the change")
	}
	// Rewriting the outcome with the same action does not announce it again.
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Decision: "OK!", Actions: []string{"Envoyer|Marie|"}}))
	if last := recorded(t, out); last[len(last)-1]["event"] != "outcome.set" {
		t.Fatalf("last event %v", last[len(last)-1]["event"])
	}
	if evs[3]["n"] != float64(1) {
		t.Fatalf("action number %v", evs[3]["n"])
	}
	// Marking an action fires once; marking it again in the same state does nothing.
	n := len(recorded(t, out))
	must[*Item](t)(s.SetActionDone("RDIR-1", "", 1, true))
	must[*Item](t)(s.SetActionDone("RDIR-1", "", 1, true))
	must[*Item](t)(s.SetActionDone("RDIR-1", "", 1, false))
	last := recorded(t, out)[n:]
	if len(last) != 2 || last[0]["event"] != "action.done" || last[1]["event"] != "action.reopened" ||
		last[0]["n"] != float64(1) || last[0]["sitting"] != "RDIR-2026-10-08" {
		t.Fatalf("action events %v", last)
	}
	// Too deep: hooks are skipped.
	t.Setenv("OJ_HOOK_DEPTH", "3")
	n = len(recorded(t, out))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Profond"}, false, ""))
	if len(recorded(t, out)) != n || !strings.Contains(warns[len(warns)-1], "skipped") {
		t.Fatal("hooks should be skipped at depth 3")
	}
}

func TestHookEventsOfSittings(t *testing.T) {
	s := withRDIR(t)
	out := filepath.Join(t.TempDir(), "events.jsonl")
	exe, _ := os.Executable()
	s.Hooks = []config.Hook{{On: []string{"sitting.frozen", "sitting.minuted", "item.deferred", "item.moved", "sitting.cancelled"},
		Run: []string{"env", "OJ_TEST_HOOK_OUT=" + out, "OJ_TEST_ROOT=" + s.Root, exe}}}
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "A"}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "B"}, true, ""))
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", nil, nil))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "Fait"}))
	must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	must[*Changed](t)(s.CancelSitting("RDIR-2026-10-15", "vacances"))
	var names []string
	for _, e := range recorded(t, out) {
		names = append(names, e["event"].(string))
	}
	want := "sitting.frozen item.deferred sitting.minuted item.moved sitting.cancelled"
	if strings.Join(names, " ") != want {
		t.Fatalf("events %q, want %q", strings.Join(names, " "), want)
	}
	if ev := recorded(t, out)[2]; !strings.HasSuffix(ev["rendered"].(string), "2026-10-08-minutes.md") {
		t.Fatalf("minuted payload %v", ev)
	}
}
