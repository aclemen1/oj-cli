package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aclemen1/ordo-cli/internal/spec"
)

// Task is one Google Task as a task CLI prints it.
type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Notes  string `json:"notes"`
	Status string `json:"status"`
	Due    string `json:"due"`
	Links  []struct {
		Type string `json:"type"`
		Link string `json:"link"`
	} `json:"links"`
}

// ParseTasks reads {"items": [...]} or a bare list.
func ParseTasks(b []byte) ([]Task, error) {
	var page struct {
		Items []Task `json:"items"`
	}
	if err := json.Unmarshal(b, &page); err == nil && page.Items != nil {
		return page.Items, nil
	}
	var list []Task
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, spec.UserError("tasks: expected the JSON of a task list, {\"items\": [...]} or [...]: %v", err)
	}
	return list, nil
}

type Imported struct {
	Created []*Item          `json:"created"`
	Skipped []map[string]any `json:"skipped"`
	DryRun  bool             `json:"dry_run,omitempty"`
}

// taskRefs: gtasks:<id>, and gmail:message/<id> for a task made from an e-mail.
func taskRefs(t Task) []string {
	refs := []string{"gtasks:" + t.ID}
	for _, l := range t.Links {
		if l.Type == "email" && l.Link != "" {
			mid := l.Link[strings.LastIndex(strings.TrimRight(l.Link, "/"), "/")+1:]
			refs = append(refs, "gmail:message/"+strings.TrimRight(mid, "/"))
		}
	}
	return refs
}

// ImportTasks turns open tasks into items of a meeting, in one commit. A task
// already imported (same gtasks: ref) is skipped, so an import can be replayed.
func (s *Store) ImportTasks(alias string, tasks []Task, withCompleted, accept, dryRun bool) (*Imported, error) {
	m, err := s.Meeting(alias)
	if err != nil {
		return nil, err
	}
	out := &Imported{Created: []*Item{}, Skipped: []map[string]any{}, DryRun: dryRun}
	known := map[string]bool{}
	existing, err := s.items(m.Alias)
	if err != nil {
		return nil, err
	}
	for _, it := range existing {
		for _, r := range it.Refs {
			known[r] = true
		}
	}
	var todo []Task
	for _, t := range tasks {
		switch {
		case t.ID == "" || strings.TrimSpace(t.Title) == "":
			out.Skipped = append(out.Skipped, map[string]any{"task": t.ID, "title": t.Title, "reason": "no id or no title"})
		case t.Status == "completed" && !withCompleted:
			out.Skipped = append(out.Skipped, map[string]any{"task": t.ID, "title": t.Title, "reason": "completed"})
		case known["gtasks:"+t.ID]:
			out.Skipped = append(out.Skipped, map[string]any{"task": t.ID, "title": t.Title, "reason": "already imported"})
		default:
			todo = append(todo, t)
		}
	}
	if dryRun || len(todo) == 0 {
		for _, t := range todo {
			out.Created = append(out.Created, &Item{Meeting: m.Alias, Title: strings.TrimSpace(t.Title), Refs: taskRefs(t), Notes: taskNotes(t)})
		}
		return out, nil
	}
	err = s.writeAs(func() (string, error) {
		m, err := s.Meeting(m.Alias)
		if err != nil {
			return "", err
		}
		sit, err := s.nextOpen(m, s.today(), true, "", "planned")
		if err != nil {
			return "", err
		}
		for _, t := range todo {
			it, err := s.addItem(m, sit, ItemInput{Title: t.Title, Refs: taskRefs(t), Notes: taskNotes(t)}, accept)
			if err != nil {
				return "", err
			}
			out.Created = append(out.Created, it)
		}
		return fmt.Sprintf("import gtasks %s (%d items)", m.Alias, len(todo)), s.saveMeeting(m)
	})
	return out, err
}

func taskNotes(t Task) string {
	n := strings.TrimSpace(t.Notes)
	if t.Due != "" && len(t.Due) >= 10 {
		if n != "" {
			n += "\n\n"
		}
		n += "Due in Google Tasks: " + t.Due[:10]
	}
	return n
}
