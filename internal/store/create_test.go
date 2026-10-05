package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aclemen1/ordo-cli/internal/config"
)

func TestCreateRef(t *testing.T) {
	s := withRDIR(t)
	must[*Meeting](t)(s.EditMeeting("RDIR", MeetingInput{Refs: []string{"office:U-RDIR"}}))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, ""))
	log := filepath.Join(t.TempDir(), "calls")
	s.Refs = map[string]config.RefSource{"office": {
		Create: []string{"sh", "-c", `echo "create $0 $1 $2" >> ` + log + `; echo '{"ok": true, "result": {"id": "U-0099"}}'`, "{title}", "{item}", "{meeting_ref}"},
		Link:   []string{"sh", "-c", `echo "link $0 $1" >> ` + log, "{meeting_ref}", "{id}"},
	}}
	c := must[*Created](t)(s.CreateRef("RDIR-1", "office"))
	if c.Ref != "office:U-0099" || !c.Linked || !contains(c.Item.Refs, "office:U-0099") {
		t.Fatalf("created %+v", c)
	}
	b, _ := os.ReadFile(log)
	if got := strings.TrimSpace(string(b)); got != "create Budget RDIR-1 U-RDIR\nlink U-RDIR U-0099" {
		t.Fatalf("calls:\n%s", got)
	}
	// Twice: the item keeps its ref, nothing runs.
	again := must[*Created](t)(s.CreateRef("RDIR-1", "office"))
	if !again.Known {
		t.Fatal("a second create should find the ref")
	}
	// office registering the dossier as an item finds the existing one.
	same := must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget (office)", Refs: []string{"office:U-0099"}}, false, ""))
	if same.ID != "RDIR-1" {
		t.Fatalf("add with a known ref made %s", same.ID)
	}
	if _, err := s.CreateRef("RDIR-1", "gmail"); kind(err) != "user_error" {
		t.Fatal("a scheme without create command is refused")
	}
	// A ref can be removed; an unknown one is refused.
	cleared := must[*Item](t)(s.EditItem("RDIR-1", ItemInput{Notes: "Contexte repris.", ClearRefs: []string{"office:U-0099"}}))
	if contains(cleared.Refs, "office:U-0099") || cleared.Notes != "Contexte repris." {
		t.Fatalf("clear ref %+v", cleared)
	}
	if _, err := s.EditItem("RDIR-1", ItemInput{ClearRefs: []string{"office:U-0099"}}); kind(err) != "user_error" {
		t.Fatal("clearing an absent ref is refused")
	}
	// The scheme defaults to the only one that can create.
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Autre"}, true, ""))
	if c := must[*Created](t)(s.CreateRef("RDIR-2", "")); c.Ref != "office:U-0099" {
		t.Fatalf("default scheme: %+v", c)
	}
	s.Refs["tracker"] = config.RefSource{Create: []string{"echo", "T-1"}}
	if _, err := s.CreateRef("RDIR-2", ""); kind(err) != "user_error" || !strings.Contains(err.Error(), "--scheme") {
		t.Fatalf("two schemes need --scheme: %v", err)
	}
	if c := must[*Created](t)(s.CreateRef("RDIR-2", "tracker")); c.Ref != "tracker:T-1" {
		t.Fatalf("named scheme: %+v", c)
	}
}
