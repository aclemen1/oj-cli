package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aclemen1/oj-cli/internal/spec"
)

type Created struct {
	Item   *Item  `json:"item"`
	Ref    string `json:"ref"`
	Known  bool   `json:"known,omitempty"`
	Linked bool   `json:"linked,omitempty"`
}

// CreateSchemes lists the ref schemes the sphere can create a target for, sorted.
func (s *Store) CreateSchemes() []string {
	var out []string
	for k, v := range s.Refs {
		if len(v.Create) > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// CreateScheme picks the scheme to create: the one asked, or the only one configured.
func (s *Store) CreateScheme(asked string) (string, error) {
	if asked != "" {
		return asked, nil
	}
	l := s.CreateSchemes()
	switch len(l) {
	case 0:
		return "", spec.UserError("no refs.<scheme>.create in the sphere's configuration: oj cannot create a target")
	case 1:
		return l[0], nil
	}
	return "", spec.UserError("several schemes can create a target (%s): pass --scheme, e.g. --scheme %s", strings.Join(l, ", "), l[0])
}

// CreateRef makes a target for an item with the sphere's create command of
// the scheme (e.g. an office dossier), adds its ref to the item, then runs the
// link command. An item that already has a ref of the scheme keeps it.
func (s *Store) CreateRef(id, scheme string) (*Created, error) {
	scheme, err := s.CreateScheme(scheme)
	if err != nil {
		return nil, err
	}
	src, ok := s.Refs[scheme]
	if !ok || len(src.Create) == 0 {
		return nil, spec.UserError("no create command for refs %q in the sphere's configuration (refs.%s.create)", scheme+":", scheme)
	}
	it, err := s.Item(id)
	if err != nil {
		return nil, err
	}
	for _, r := range it.Refs {
		if strings.HasPrefix(r, scheme+":") {
			return &Created{Item: it, Ref: r, Known: true}, nil
		}
	}
	m, err := s.Meeting(it.Meeting)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{
		"{title}": it.Title, "{item}": it.ID, "{meeting}": m.Alias, "{meeting_title}": m.Title,
		"{sitting}": it.Sitting, "{notes}": it.Notes, "{expected}": it.Expected,
		"{meeting_ref}": m.Alias,
	}
	for _, r := range m.Refs {
		if v, ok := strings.CutPrefix(r, scheme+":"); ok {
			vars["{meeting_ref}"] = v
			break
		}
	}
	out, err := s.run(src.Create, vars)
	if err != nil {
		return nil, fmt.Errorf("create %s for %s: %w", scheme, it.ID, err)
	}
	target := createdID(out)
	if target == "" {
		return nil, fmt.Errorf("create %s for %s: the command printed no id", scheme, it.ID)
	}
	ref := scheme + ":" + target
	if it, err = s.EditItem(it.ID, ItemInput{Refs: []string{ref}}); err != nil {
		return nil, err
	}
	res := &Created{Item: it, Ref: ref}
	if len(src.Link) > 0 {
		vars["{id}"] = target
		if _, err := s.run(src.Link, vars); err != nil {
			s.warn(fmt.Sprintf("link %s for %s: %v", ref, it.ID, err))
		} else {
			res.Linked = true
		}
	}
	return res, nil
}

// createdID reads the id a create command printed: JSON with result.id or id, else the last line.
func createdID(out string) string {
	var env struct {
		ID     string `json:"id"`
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(out), &env) == nil {
		if env.Result.ID != "" {
			return env.Result.ID
		}
		if env.ID != "" {
			return env.ID
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (s *Store) run(argv []string, vars map[string]string) (string, error) {
	args := make([]string, len(argv))
	for i, a := range argv {
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(b)), nil
}
