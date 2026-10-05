package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RefShown is what a ref's command says about its target.
type RefShown struct {
	Ref   string `json:"ref"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// CanShowRef tells whether the sphere knows how to summarise a ref.
func (s *Store) CanShowRef(ref string) bool {
	scheme, _, ok := strings.Cut(ref, ":")
	src, known := s.Refs[scheme]
	return ok && known && len(src.Show) > 0
}

// ShowRef runs the command the sphere names for the ref's scheme.
func (s *Store) ShowRef(ref string) RefShown {
	out := RefShown{Ref: ref}
	scheme, id, ok := strings.Cut(ref, ":")
	src, known := s.Refs[scheme]
	if !ok || !known || len(src.Show) == 0 {
		out.Error = fmt.Sprintf("no command for refs %q in the sphere's configuration", scheme+":")
		return out
	}
	args := make([]string, len(src.Show))
	for i, a := range src.Show {
		args[i] = strings.ReplaceAll(a, "{id}", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = s.Root
	cmd.Env = append(os.Environ(), "OJ_SPHERE="+s.Sphere)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		why := strings.TrimSpace(stderr.String())
		if why == "" {
			why = strings.TrimSpace(string(b))
		}
		out.Error = strings.TrimSpace(fmt.Sprintf("%s failed (%v): %s", args[0], err, why))
		return out
	}
	if len(b) > 64<<10 {
		b = b[:64<<10]
	}
	out.Text = strings.TrimSpace(string(b))
	return out
}

// CanOpenRef tells whether the sphere knows how to jump to a ref's target.
func (s *Store) CanOpenRef(ref string) bool {
	scheme, _, ok := strings.Cut(ref, ":")
	return ok && len(s.Refs[scheme].Open) > 0
}

// OpenRef runs the sphere's open command for the ref (e.g. focus a dossier's session).
func (s *Store) OpenRef(ref string) error {
	scheme, id, ok := strings.Cut(ref, ":")
	if !ok || len(s.Refs[scheme].Open) == 0 {
		return fmt.Errorf("no open command for refs %q in the sphere's configuration (refs.%s.open)", scheme+":", scheme)
	}
	_, err := s.run(s.Refs[scheme].Open, map[string]string{"{id}": id})
	return err
}
