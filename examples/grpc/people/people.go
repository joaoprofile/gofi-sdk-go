// Package people is the domain shared by the gRPC and HTTP transports: the
// Person entity, its photo and a thread-safe in-memory store. It knows
// nothing about gRPC or HTTP; both map its errs.AppError values to their own
// status codes.
package people

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
)

// MaxPhotoSize bounds an uploaded photo.
const MaxPhotoSize = 5 << 20 // 5 MiB

var (
	ErrPersonNotFound = errs.RegisterNotFound("PERSON_NOT_FOUND", "person %s not found")
	ErrPhotoNotFound  = errs.RegisterNotFound("PHOTO_NOT_FOUND", "person %s has no photo")
	ErrInvalidPerson  = errs.RegisterValidation("INVALID_PERSON", "invalid person: %s")
	ErrInvalidPhoto   = errs.RegisterValidation("INVALID_PHOTO", "invalid photo: %s")
)

// photoTypes are the accepted image formats, detected from the bytes.
var photoTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

type Address struct {
	Street  string `json:"street,omitempty"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

type Person struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	BirthDate time.Time  `json:"birth_date,omitzero"`
	Phones    []string   `json:"phones,omitempty"`
	Address   Address    `json:"address,omitzero"`
	CreatedAt time.Time  `json:"created_at"`
	Photo     *PhotoInfo `json:"photo,omitempty"`
}

// PhotoInfo describes a stored photo; the bytes are served separately.
type PhotoInfo struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// NewPerson is what a caller provides to create a person.
type NewPerson struct {
	Name      string
	Email     string
	BirthDate time.Time
	Phones    []string
	Address   Address
}

// Store keeps people and their photos in memory.
type Store struct {
	mu     sync.RWMutex
	nextID int
	people map[string]Person
	photos map[string][]byte
}

func NewStore() *Store {
	return &Store{nextID: 1, people: map[string]Person{}, photos: map[string][]byte{}}
}

func (s *Store) Create(in NewPerson) (Person, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Person{}, ErrInvalidPerson.New("name is required")
	}
	if _, err := mail.ParseAddress(in.Email); err != nil {
		return Person{}, ErrInvalidPerson.New(fmt.Sprintf("email %q is not valid", in.Email))
	}
	if !in.BirthDate.IsZero() && in.BirthDate.After(time.Now()) {
		return Person{}, ErrInvalidPerson.New("birth date is in the future")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	p := Person{
		ID:        "p-" + strconv.Itoa(s.nextID),
		Name:      name,
		Email:     in.Email,
		BirthDate: in.BirthDate,
		Phones:    slices.Clone(in.Phones),
		Address:   in.Address,
		CreatedAt: time.Now().UTC(),
	}
	s.nextID++
	s.people[p.ID] = p
	return p, nil
}

func (s *Store) Get(id string) (Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.people[id]
	if !ok {
		return Person{}, ErrPersonNotFound.New(id)
	}
	return p, nil
}

// List returns the people whose name contains nameContains (any case), in
// creation order.
func (s *Store) List(nameContains string) []Person {
	needle := strings.ToLower(strings.TrimSpace(nameContains))
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Person, 0, len(s.people))
	for _, p := range s.people {
		if needle == "" || strings.Contains(strings.ToLower(p.Name), needle) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Person) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// SetPhoto validates the image (type detected from the bytes, size) and
// stores it as the person's photo.
func (s *Store) SetPhoto(id string, data []byte) (PhotoInfo, error) {
	if len(data) == 0 {
		return PhotoInfo{}, ErrInvalidPhoto.New("empty image")
	}
	if len(data) > MaxPhotoSize {
		return PhotoInfo{}, ErrInvalidPhoto.New(fmt.Sprintf("larger than %d bytes", MaxPhotoSize))
	}
	contentType := http.DetectContentType(data)
	if !slices.Contains(photoTypes, contentType) {
		return PhotoInfo{}, ErrInvalidPhoto.New(fmt.Sprintf("%s is not an accepted image type", contentType))
	}
	sum := sha256.Sum256(data)
	info := PhotoInfo{ContentType: contentType, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}

	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.people[id]
	if !ok {
		return PhotoInfo{}, ErrPersonNotFound.New(id)
	}
	p.Photo = &info
	s.people[id] = p
	s.photos[id] = slices.Clone(data)
	return info, nil
}

// Photo returns the photo metadata and bytes; the bytes must not be modified.
func (s *Store) Photo(id string) (PhotoInfo, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.people[id]
	if !ok {
		return PhotoInfo{}, nil, ErrPersonNotFound.New(id)
	}
	if p.Photo == nil {
		return PhotoInfo{}, nil, ErrPhotoNotFound.New(id)
	}
	return *p.Photo, s.photos[id], nil
}
