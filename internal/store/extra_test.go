package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A field written by a newer version survives a rewrite by this one.
func TestUnknownFieldsSurvive(t *testing.T) {
	s := withRDIR(t)
	p := filepath.Join(s.Root, "RDIR", "meeting.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "\n---\n", "\nfuture_field:\n  - a: 1\n---\n", 1)), 0o644)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, "")) // rewrites meeting.md (counter)
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "future_field:") || !strings.Contains(string(after), "a: 1") {
		t.Fatalf("unknown field lost:\n%s", after)
	}
	ip := filepath.Join(s.Root, "RDIR", "items", "RDIR-1.md")
	ib, _ := os.ReadFile(ip)
	os.WriteFile(ip, []byte(strings.Replace(string(ib), "\n---\n", "\nlater: yes\n---\n", 1)), 0o644)
	must[*Item](t)(s.EditItem("RDIR-1", ItemInput{Duration: "20m"}))
	if ia, _ := os.ReadFile(ip); !strings.Contains(string(ia), "later:") {
		t.Fatalf("unknown item field lost:\n%s", ia)
	}
}
