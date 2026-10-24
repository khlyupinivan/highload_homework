package store

import (
	"errors"
	"sync"
)

type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

type Store struct {
	mu    sync.RWMutex
	items map[string]Item
}

func New() *Store { return &Store{items: map[string]Item{}} }

func (s *Store) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, it)
	}
	return out
}

func (s *Store) Get(id string) (Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	it, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

func (s *Store) Create(it Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[it.ID]; ok {
		return ErrExists
	}
	s.items[it.ID] = it
	return nil
}

func (s *Store) Update(it Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[it.ID]; !ok {
		return Item{}, ErrNotFound
	}
	s.items[it.ID] = it
	return it, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}
