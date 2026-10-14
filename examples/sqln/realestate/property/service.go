package property

import (
	"context"
	"sync/atomic"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

// Service searches and lists properties on the global sqln connection.
type Service struct {
	dict atomic.Pointer[Dictionary]
	ip   atomic.Pointer[Interpreter]
}

func NewService() *Service { return &Service{} }

// Start loads the reference data and fills the lexicon of the spelling
// suggestions. Call it once the database is open (after gofi's Build).
func (s *Service) Start(ctx context.Context) error {
	d, err := LoadDictionary(ctx)
	if err != nil {
		return err
	}
	s.dict.Store(d)
	s.ip.Store(NewInterpreter(d))
	return s.refreshLexicon(ctx)
}

// Dictionary returns the reference data loaded by Start.
func (s *Service) Dictionary() *Dictionary { return s.dict.Load() }

func (s *Service) interpreter() *Interpreter { return s.ip.Load() }

// refreshLexicon rebuilds the words "você quis dizer" chooses from. It reads
// the whole table: fine for a sample catalog at startup and after a listing;
// a large catalog runs it from a periodic job instead.
func (s *Service) refreshLexicon(ctx context.Context) error {
	conn, err := connection.Global()
	if err != nil {
		return err
	}
	_, err = fts.RefreshLexicon(ctx, conn.DB(), conn.Dialect(), searchIndex)
	return err
}
