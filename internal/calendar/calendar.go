// Package calendar reads events from a calendar source: an ICS file or URL,
// or a command that prints events as JSON.
package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Event is one occurrence in a calendar.
type Event struct {
	UID         string    `json:"uid"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Title       string    `json:"title"`
	Location    string    `json:"location,omitempty"`
	Status      string    `json:"status,omitempty"` // confirmed, tentative, cancelled
	RecurringID string    `json:"recurring_id,omitempty"`
	// Original is the start the recurrence gave this occurrence before it was moved.
	Original time.Time `json:"original_start,omitzero"`
}

func (e Event) Cancelled() bool { return strings.EqualFold(e.Status, "cancelled") }

// Source is a configured calendar.
type Source struct {
	Type string   `yaml:"type"` // ics or command
	Run  []string `yaml:"run,omitempty"`
	URL  string   `yaml:"url,omitempty"`
	Path string   `yaml:"path,omitempty"`
}

// Events returns the events between from and to.
func (s Source) Events(ctx context.Context, from, to time.Time) ([]Event, error) {
	switch s.Type {
	case "command":
		return s.command(ctx, from, to)
	case "ics":
		b, err := s.icsBytes(ctx)
		if err != nil {
			return nil, err
		}
		return ParseICS(b, from, to)
	}
	return nil, fmt.Errorf("calendar type %q: expected ics or command", s.Type)
}

// command runs the source's command with OJ_FROM and OJ_TO (RFC 3339)
// and reads a JSON list of events from its output.
func (s Source) command(ctx context.Context, from, to time.Time) ([]Event, error) {
	if len(s.Run) == 0 {
		return nil, fmt.Errorf("calendar of type command needs run: [program, args…]")
	}
	cmd := exec.CommandContext(ctx, s.Run[0], s.Run[1:]...)
	cmd.Env = append(os.Environ(), "OJ_FROM="+from.Format(time.RFC3339), "OJ_TO="+to.Format(time.RFC3339))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("calendar command %s: %v: %s", s.Run[0], err, strings.TrimSpace(stderr.String()))
	}
	return ParseJSON(out)
}

// ParseJSON reads [{uid, start, end, title, location, status, recurring_id, original_start}];
// start, end and original_start are RFC 3339 times or YYYY-MM-DD dates.
func ParseJSON(b []byte) ([]Event, error) {
	var raw []map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("calendar events: expected a JSON list: %v", err)
	}
	var out []Event
	for i, r := range raw {
		str := func(k string) string { v, _ := r[k].(string); return v }
		e := Event{UID: str("uid"), Title: str("title"), Location: str("location"), Status: str("status"), RecurringID: str("recurring_id")}
		var err error
		if e.Start, err = parseTime(str("start")); err != nil {
			return nil, fmt.Errorf("event %d: start: %v", i, err)
		}
		if v := str("end"); v != "" {
			if e.End, err = parseTime(v); err != nil {
				return nil, fmt.Errorf("event %d: end: %v", i, err)
			}
		}
		if v := str("original_start"); v != "" {
			if e.Original, err = parseTime(v); err != nil {
				return nil, fmt.Errorf("event %d: original_start: %v", i, err)
			}
		}
		if e.UID == "" {
			return nil, fmt.Errorf("event %d has no uid", i)
		}
		out = append(out, e)
	}
	return out, nil
}

func parseTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", v, time.Local)
}

func (s Source) icsBytes(ctx context.Context) ([]byte, error) {
	if s.Path != "" {
		return os.ReadFile(s.Path)
	}
	if s.URL == "" {
		return nil, fmt.Errorf("calendar of type ics needs path or url")
	}
	url := strings.Replace(s.URL, "webcal://", "https://", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}
