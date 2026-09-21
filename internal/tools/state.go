package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// State is what the launcher remembers: pinned favourites and how often and how recently each tool ran.
type State struct {
	Pinned []string            `json:"pinned,omitempty"`
	Used   map[string]UseCount `json:"used,omitempty"`
}

type UseCount struct {
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

// LoadState reads the state file; a missing or damaged file is an empty state (it is only a convenience).
func LoadState(path string) State {
	var s State
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Used == nil {
		s.Used = map[string]UseCount{}
	}
	return s
}

func (s State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) IsPinned(id string) bool {
	for _, p := range s.Pinned {
		if p == id {
			return true
		}
	}
	return false
}

// TogglePin pins or unpins a tool and reports whether it is now pinned.
func (s *State) TogglePin(id string) bool {
	for i, p := range s.Pinned {
		if p == id {
			s.Pinned = append(s.Pinned[:i], s.Pinned[i+1:]...)
			return false
		}
	}
	s.Pinned = append(s.Pinned, id)
	return true
}

func (s *State) Record(id string, at time.Time) {
	if s.Used == nil {
		s.Used = map[string]UseCount{}
	}
	u := s.Used[id]
	u.Count++
	u.Last = at
	s.Used[id] = u
}

// Order puts pinned tools first (in the order pinned), then the ones used lately (newest first), then the
// rest in catalogue order. Nothing is dropped.
func (s State) Order(list []Tool) []Tool {
	rank := func(t Tool) (int, int) {
		for i, p := range s.Pinned {
			if p == t.ID {
				return 0, i
			}
		}
		if _, ok := s.Used[t.ID]; ok {
			return 1, 0
		}
		return 2, 0
	}
	out := append([]Tool(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, pi := rank(out[i])
		rj, pj := rank(out[j])
		if ri != rj {
			return ri < rj
		}
		if ri == 0 {
			return pi < pj
		}
		if ri == 1 {
			return s.Used[out[i].ID].Last.After(s.Used[out[j].ID].Last)
		}
		return false
	})
	return out
}
