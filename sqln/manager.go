package sqln

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/joaoprofile/gofi/sqln/cache"
	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/joaoprofile/gofi/sqln/mapping"
	"github.com/joaoprofile/gofi/sqln/pagination"
)

// Manager defines a read-only query interface that can be mocked in tests.
type Manager[T any] interface {
	ExecuteListQuery(db *sql.DB) ([]T, error)
	ExecuteUniqueResultQuery(db *sql.DB) (*T, error)
	ExecutePagedQuery(db *sql.DB) (*Page[T], error)
}

type manager[T any] struct {
	ctx           context.Context
	conn          *connection.Connection // optional injected connection; if nil, falls back to global
	cache         *cache.Cache[T]
	page          *pagination.PageRequest
	query         string
	args          []any
	criteriaQuery *criteria.Query // non-nil when created via FindFromCriteria; built lazily
}

//  Constructors

func Find[T any](ctx context.Context, query string, params ...any) *manager[T] {
	return &manager[T]{ctx: ctx, query: query, args: params}
}

func FindWithFilter[T any](ctx context.Context, params *QueryParam) *manager[T] {
	return &manager[T]{ctx: ctx, query: params.Query, args: params.Params}
}

func NewCustomQuery[T any](ctx context.Context, query string, params ...any) *manager[T] {
	return &manager[T]{ctx: ctx, query: query, args: params}
}

func FindFromCriteria[T any](ctx context.Context, q *criteria.Query) *manager[T] {
	return &manager[T]{ctx: ctx, criteriaQuery: q}
}

// Chainable options

// WithConnection injects a specific connection to use instead of the global singleton.
// Enables multi-tenant or multi-database scenarios without relying on connection.SetGlobal.
func (q *manager[T]) WithConnection(conn *connection.Connection) *manager[T] {
	q.conn = conn
	return q
}

func (q *manager[T]) WithPage(page *pagination.PageRequest) *manager[T] {
	q.page = page
	return q
}

func (q *manager[T]) WithCache(c *cache.Cache[T]) *manager[T] {
	q.cache = c
	return q
}

// Execution

func (q *manager[T]) Execute() (*T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteUniqueResultQuery(conn.DB())
}

func (q *manager[T]) List() ([]T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteListQuery(conn.DB())
}

func (q *manager[T]) UniqueResult() (*T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteUniqueResultQuery(conn.DB())
}

func (q *manager[T]) PagedList() (*Page[T], error) {
	conn := q.connection()
	q.resolveFromCriteria(conn) // uses BuildBase when page is set
	return q.ExecutePagedQuery(conn.DB())
}

//  Manager interface (testable / mockable)

func (q *manager[T]) ExecuteListQuery(instance *sql.DB) ([]T, error) {
	q.resolveFromCriteriaLazy()
	if err := q.validate(instance); err != nil {
		return nil, err
	}

	if q.cache == nil {
		return q.fetchList(instance)
	}

	result, err := q.cache.List(q.ctx)
	if result == nil || err != nil {
		return q.fetchList(instance)
	}

	return result, nil
}

func (q *manager[T]) ExecuteUniqueResultQuery(instance *sql.DB) (*T, error) {
	q.resolveFromCriteriaLazy()
	if err := q.validate(instance); err != nil {
		return nil, err
	}

	if q.cache == nil {
		return q.fetchUniqueResult(instance)
	}

	result, err := q.cache.UniqueResult(q.ctx)
	if result == nil || err != nil {
		return q.fetchUniqueResult(instance)
	}
	return result, nil
}

func (q *manager[T]) ExecutePagedQuery(instance *sql.DB) (*Page[T], error) {
	q.resolveFromCriteriaLazy()
	if q.page == nil {
		return nil, fmt.Errorf(ErrPageIsEmpty)
	}
	if err := q.validate(instance); err != nil {
		return nil, err
	}

	var result Page[T]
	var err error
	result.TotalElements, err = q.pageTotal(instance)
	if err != nil {
		return nil, err
	}
	result.TotalPages = (result.TotalElements + uint64(q.page.Limit-1)) / uint64(q.page.Limit)
	result.Size = uint64(q.page.Limit)
	result.Number = uint64(q.page.Page)
	result.NumberOfElements = uint64(q.page.Limit)

	result.Content, err = q.fetchPagedList(instance)
	return &result, err
}

//  Internal fetch helpers

func (q *manager[T]) fetchList(instance *sql.DB) ([]T, error) {
	rows, err := NewQuery().FetchRows(q.ctx, instance, q.query, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list, err := mapping.GetContentList[T](rows)
	if err != nil {
		return nil, err
	}

	if q.cache != nil {
		q.cache.Set(q.ctx, list)
	}

	return list, nil
}

func (q *manager[T]) fetchUniqueResult(instance *sql.DB) (*T, error) {
	model := new(T)

	if err := NewQuery().FetchRow(q.ctx, instance, q.query, q.args...).
		Scan(mapping.GetMappedCols(model)...); err != nil && err != sql.ErrNoRows {
		return nil, err
	} else if err == sql.ErrNoRows {
		return nil, nil
	}

	if q.cache != nil {
		q.cache.Set(q.ctx, model)
	}

	return model, nil
}

func (q *manager[T]) fetchPagedList(instance *sql.DB) ([]T, error) {
	conn := q.connection()

	sqlQuery := conn.Dialect().BuildPagination(
		q.query,
		q.page.GetOrder(),
		q.page.Limit,
		q.page.Page*q.page.Limit,
	)

	rows, err := NewQuery().FetchRows(q.ctx, instance, sqlQuery, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return mapping.GetContentList[T](rows)
}

func (q *manager[T]) pageTotal(instance *sql.DB) (uint64, error) {
	conn := q.connection()
	sqlQuery := conn.Dialect().BuildCount(q.query)

	var result uint64
	err := NewQuery().FetchRow(q.ctx, instance, sqlQuery, q.args...).Scan(&result)

	return result, err
}

// Criteria resolution

// resolveFromCriteria builds the SQL from a criteria.Query using the provided
// connection's dialect. Called from the high-level execution methods (List,
// UniqueResult, PagedList) which already hold a connection reference.
//
// When page is set, BuildBase is used so that ORDER BY / LIMIT / OFFSET are
// handled by dialect.BuildPagination instead.
func (q *manager[T]) resolveFromCriteria(conn *connection.Connection) {
	if q.criteriaQuery == nil {
		return
	}
	if q.page != nil {
		q.query, q.args = q.criteriaQuery.BuildBase(conn.Dialect())
	} else {
		q.query, q.args = q.criteriaQuery.Build(conn.Dialect())
	}
	q.criteriaQuery = nil
}

// resolveFromCriteriaLazy is used by the Manager interface methods
// (ExecuteListQuery, etc.) which receive a *sql.DB but not a Connection.
// Uses the injected connection when available, otherwise falls back to global.
func (q *manager[T]) resolveFromCriteriaLazy() {
	if q.criteriaQuery == nil {
		return
	}
	q.resolveFromCriteria(q.connection())
}

//  Utilities

func (q *manager[T]) validate(instance *sql.DB) error {
	if instance == nil {
		return errors.New(ErrDatabaseNotInitialized)
	}
	if q.query == "" {
		return errors.New(ErrMsgQueryIsEmpty)
	}
	return nil
}

func (q *manager[T]) connection() *connection.Connection {
	if q.conn != nil {
		return q.conn
	}
	conn, err := connection.Global()
	if err != nil {
		panic(err)
	}
	return conn
}
