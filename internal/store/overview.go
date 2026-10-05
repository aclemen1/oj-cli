package store

// Overview is a meeting's sittings, each with its agenda, and its open items
// that have no sitting yet.
type Overview struct {
	Meeting   string    `json:"meeting"`
	Title     string    `json:"title"`
	Sittings  []*Agenda `json:"sittings"`
	Unplanned []*Item   `json:"unplanned"`
}

// Overviews gives the sittings of one meeting (alias) or of all, written ones
// from since (or still open when since is empty) and the next ahead dates.
func (s *Store) Overviews(alias, since string, ahead int) ([]Overview, error) {
	var ms []*Meeting
	if alias != "" {
		m, err := s.Meeting(alias)
		if err != nil {
			return nil, err
		}
		ms = []*Meeting{m}
	} else {
		var err error
		if ms, err = s.Meetings(); err != nil {
			return nil, err
		}
	}
	out := []Overview{}
	for _, m := range ms {
		ov := Overview{Meeting: m.Alias, Title: m.Title, Sittings: []*Agenda{}, Unplanned: []*Item{}}
		sits, err := s.Sittings(m, since, ahead, "all")
		if err != nil {
			return nil, err
		}
		for _, sit := range sits {
			a, err := s.Agenda(sit.ID)
			if err != nil {
				return nil, err
			}
			ov.Sittings = append(ov.Sittings, a)
		}
		items, err := s.items(m.Alias)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Sitting == "" && (it.State == "proposed" || it.State == "accepted" || it.State == "deferred") {
				ov.Unplanned = append(ov.Unplanned, it)
			}
		}
		out = append(out, ov)
	}
	return out, nil
}
