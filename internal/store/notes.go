package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aclemen1/oj-cli/internal/spec"
)

// CanAddNote tells whether the sphere records notes outside the item's body (notes.add).
func (s *Store) CanAddNote() bool { return len(s.NotesAdd) > 0 }

// AddNote records a note about an item with the sphere's notes.add command;
// the item's file is left as it is.
func (s *Store) AddNote(id, text string) (*Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, spec.UserError("a note needs a text")
	}
	if !s.CanAddNote() {
		return nil, spec.UserError("the sphere %s has no notes.add command; notes stay in the item's body (oj item edit --notes)", s.Sphere)
	}
	it, err := s.Item(id)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{"{ref}": "oj:" + it.ID, "{sphere}": s.Sphere, "{text}": text}
	args := make([]string, len(s.NotesAdd))
	for i, a := range s.NotesAdd {
		for k, v := range vars {
			a = strings.ReplaceAll(a, k, v)
		}
		args[i] = a
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = s.Root
	cmd.Env = append(os.Environ(), "OJ_SPHERE="+s.Sphere)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err != nil {
		why := strings.TrimSpace(stderr.String())
		if why == "" {
			why = strings.TrimSpace(string(out))
		}
		return nil, fmt.Errorf("notes.add (%s) failed: %v: %s", args[0], err, why)
	}
	return it, nil
}
