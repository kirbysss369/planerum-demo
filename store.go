package hours

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("program not found")
var ErrConflict = errors.New("program revision conflict")

type Store interface {
	Get(ctx context.Context, id string) (Program, error)
	Put(ctx context.Context, expectedRevision int, p Program) error
}

// MemoryStore supports local tests and explicit demo mode only.
type MemoryStore struct {
	mu       sync.Mutex
	programs map[string][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{programs: map[string][]byte{}} }

func (s *MemoryStore) Get(_ context.Context, id string) (Program, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.programs[id]
	if !ok {
		return Program{}, ErrNotFound
	}
	var p Program
	return p, json.Unmarshal(data, &p)
}

func (s *MemoryStore) Put(_ context.Context, expectedRevision int, p Program) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.programs[p.ID]; ok {
		var prior Program
		if err := json.Unmarshal(data, &prior); err != nil {
			return err
		}
		if prior.Revision != expectedRevision {
			return ErrConflict
		}
	} else if expectedRevision != 0 {
		return ErrConflict
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	s.programs[p.ID] = data
	return nil
}
