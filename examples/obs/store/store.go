// Package store keeps orders in memory; it stands in for a database.
package store

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrNotFound = errors.New("order not found")

type Status string

const (
	StatusPaid     Status = "paid"
	StatusNotified Status = "notified"
	StatusArchived Status = "archived"
)

type Order struct {
	ID            string    `json:"id"`
	Amount        float64   `json:"amount"`
	PaymentMethod string    `json:"payment_method"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type Store struct {
	mu     sync.RWMutex
	seq    int
	orders map[string]*Order
}

func New() *Store { return &Store{orders: map[string]*Order{}} }

func (s *Store) Create(amount float64, method string) Order {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	o := &Order{
		ID:            fmt.Sprintf("ord-%d", s.seq),
		Amount:        amount,
		PaymentMethod: method,
		Status:        StatusPaid,
		CreatedAt:     time.Now(),
	}
	s.orders[o.ID] = o
	return *o
}

func (s *Store) Get(id string) (Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	return *o, nil
}

func (s *Store) SetStatus(id string, status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return ErrNotFound
	}
	o.Status = status
	return nil
}

// ListByStatus returns the IDs of orders in status created before cutoff.
func (s *Store) ListByStatus(status Status, cutoff time.Time) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for id, o := range s.orders {
		if o.Status == status && o.CreatedAt.Before(cutoff) {
			ids = append(ids, id)
		}
	}
	return ids
}
