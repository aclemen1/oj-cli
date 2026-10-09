package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CitedSection is what one command of the sphere's cited block says about an object.
type CitedSection struct {
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// CanCite tells whether the sphere declares an aggregated view.
func (s *Store) CanCite() bool { return len(s.Cited) > 0 }

// CitedOf runs every command of the sphere's cited block for ref (oj:RDIR-17),
// in parallel, and returns their sections in the configured order.
func (s *Store) CitedOf(ref string) []CitedSection {
	out := make([]CitedSection, len(s.Cited))
	var wg sync.WaitGroup
	for i, c := range s.Cited {
		out[i].Title = c.Title
		if len(c.Run) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := make([]string, len(c.Run))
			for j, a := range c.Run {
				args[j] = strings.ReplaceAll(strings.ReplaceAll(a, "{ref}", ref), "{sphere}", s.Sphere)
			}
			out[i].Text, out[i].Error = s.runText(args)
		}()
	}
	wg.Wait()
	return out
}

// runText runs a command that prints text for a person; it returns the text
// (at most 64 KiB) or what went wrong.
func (s *Store) runText(args []string) (text, failure string) {
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
		return "", strings.TrimSpace(fmt.Sprintf("%s failed (%v): %s", args[0], err, why))
	}
	if len(b) > 64<<10 {
		b = b[:64<<10]
	}
	return strings.TrimSpace(string(b)), ""
}
