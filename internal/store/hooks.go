package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/aclemen1/ordo-cli/internal/config"
)

// Events a hook can listen to.
var Events = []string{
	"item.added", "item.accepted", "item.deferred", "item.moved", "item.dropped",
	"outcome.set", "action.added",
	"sitting.moved", "sitting.cancelled", "sitting.frozen", "sitting.held", "sitting.minuted",
}

// MaxHookDepth stops a hook that calls ordo from starting hooks without end.
const MaxHookDepth = 3

type hookEvent struct {
	name, meeting string
	payload       map[string]any
}

// emit queues an event; it runs once the write is committed and unlocked.
func (s *Store) emit(name, meeting string, payload map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, hookEvent{name, meeting, payload})
}

func (s *Store) takePending() []hookEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := s.pending
	s.pending = nil
	return ev
}

func hookWanted(h config.Hook, e hookEvent) bool {
	if len(h.Meetings) > 0 && !contains(h.Meetings, e.meeting) {
		return false
	}
	return contains(h.On, e.name) || contains(h.On, "*")
}

// fire runs the hooks of the events, in order. A failing hook is reported
// through Warn; it never undoes what was written.
func (s *Store) fire(events []hookEvent) {
	if len(events) == 0 || len(s.Hooks) == 0 {
		return
	}
	depth, _ := strconv.Atoi(os.Getenv("ORDO_HOOK_DEPTH"))
	if depth >= MaxHookDepth {
		s.warn(fmt.Sprintf("hooks skipped: ORDO_HOOK_DEPTH is %d", depth))
		return
	}
	for _, e := range events {
		for _, h := range s.Hooks {
			if len(h.Run) == 0 || !hookWanted(h, e) {
				continue
			}
			if err := s.runHook(h, e, depth+1); err != nil {
				s.warn(fmt.Sprintf("hook %s on %s: %v", h.Run[0], e.name, err))
			}
		}
	}
}

func (s *Store) runHook(h config.Hook, e hookEvent, depth int) error {
	body := map[string]any{"event": e.name, "sphere": s.Sphere, "meeting": e.meeting,
		"at": s.Now().Format(time.RFC3339), "by": s.By}
	for k, v := range e.payload {
		body[k] = v
	}
	in, err := json.Marshal(body)
	if err != nil {
		return err
	}
	timeout := 30 * time.Second
	if h.Timeout != "" {
		if d, err := time.ParseDuration(h.Timeout); err == nil {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.Run[0], h.Run[1:]...)
	cmd.Dir = s.Root
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = append(os.Environ(), "ORDO_SPHERE="+s.Sphere, "ORDO_EVENT="+e.name, "ORDO_MEETING="+e.meeting,
		"ORDO_HOOK_DEPTH="+strconv.Itoa(depth))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s *Store) warn(m string) {
	if s.Warn != nil {
		s.Warn(m)
	}
}
