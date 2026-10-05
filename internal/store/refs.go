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
	cmd.Env = append(os.Environ(), "ORDO_SPHERE="+s.Sphere)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		out.Error = strings.TrimSpace(fmt.Sprintf("%v: %s", err, stderr.String()))
		return out
	}
	if len(b) > 64<<10 {
		b = b[:64<<10]
	}
	out.Text = strings.TrimSpace(string(b))
	return out
}
