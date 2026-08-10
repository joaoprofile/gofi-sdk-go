package criteria

import (
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi/sqln/driver"
)

// Query is a declarative, dialect-aware SQL query builder.
//
// Use From() as the entry point and chain clauses fluently.
// Call Build(dialect) to obtain the SQL string and bound parameters for direct execution,
// or BuildBase(dialect) when the ORDER BY / LIMIT / OFFSET are handled externally
// (e.g., via manager pagination).
//
// Example (direct use):
//
//	q := criteria.From("users", "u").
//	    Select("u.id", "u.name", "u.email").
//	    LeftJoin("orders", "o", "o.user_id = u.id").
//	    Where(
//	        criteria.Eq("u.active", true),
//	        criteria.And(),
//	        criteria.In("u.role", []string{"admin", "moderator"}),
//	    ).
//	    GroupBy("u.id", "u.name").
//	    Having(criteria.Gt("COUNT(o.id)", 0)).
//	    OrderBy(criteria.Asc("u.name"), criteria.Desc("u.id")).
//	    Limit(15).Offset(0)
//
//	sql, params := q.Build(dialect)
//
// Example (paged via manager — ORDER BY goes in PageRequest, not here):
//
//	sqln.FindFromCriteria[User](ctx,
//	    criteria.From("users", "u").
//	        Where(criteria.Eq("u.active", true)),
//	).WithPage(pageRequest).PagedList()
type Query struct {
	selects []string
	table   string
	alias   string
	joins   []joinClause
	where   []Predicate
	group   []string
	having  []Predicate
	order   []Order
	limit   int
	offset  int
}

type joinClause struct {
	joinType string // "JOIN" | "LEFT JOIN" | "RIGHT JOIN"
	table    string
	alias    string
	on       string
}

// From creates a new Query rooted at the given table with an optional alias.
// Pass an empty string for alias when none is needed.
func From(table, alias string) *Query {
	return &Query{table: table, alias: alias}
}

// Select specifies the columns included in the SELECT clause.
// If never called, SELECT * is generated.
func (q *Query) Select(fields ...string) *Query {
	q.selects = append(q.selects, fields...)
	return q
}

// Join adds an INNER JOIN clause.
// alias may be empty.
func (q *Query) Join(table, alias, on string) *Query {
	q.joins = append(q.joins, joinClause{joinType: "JOIN", table: table, alias: alias, on: on})
	return q
}

// LeftJoin adds a LEFT JOIN clause.
func (q *Query) LeftJoin(table, alias, on string) *Query {
	q.joins = append(q.joins, joinClause{joinType: "LEFT JOIN", table: table, alias: alias, on: on})
	return q
}

// LeftJoinLateral adds a LEFT JOIN LATERAL clause over a subquery correlated with the
// outer row. The ON condition is always TRUE: a lateral subquery expresses its own
// correlation in its WHERE, so an outer ON would only ever be redundant.
// subquery is the parenthesized SELECT, e.g. "(SELECT x FROM t WHERE t.id = p.id LIMIT 1)".
func (q *Query) LeftJoinLateral(subquery, alias string) *Query {
	q.joins = append(q.joins, joinClause{joinType: "LEFT JOIN LATERAL", table: subquery, alias: alias, on: "TRUE"})
	return q
}

// RightJoin adds a RIGHT JOIN clause.
func (q *Query) RightJoin(table, alias, on string) *Query {
	q.joins = append(q.joins, joinClause{joinType: "RIGHT JOIN", table: table, alias: alias, on: on})
	return q
}

// Where appends predicates to the WHERE clause.
// Adjacent predicates without an explicit And()/Or() connector are implicitly joined with AND.
func (q *Query) Where(predicates ...Predicate) *Query {
	q.where = append(q.where, predicates...)
	return q
}

// GroupBy appends fields to the GROUP BY clause.
func (q *Query) GroupBy(fields ...string) *Query {
	q.group = append(q.group, fields...)
	return q
}

// Having appends predicates to the HAVING clause.
// Implicit AND rules are the same as Where.
func (q *Query) Having(predicates ...Predicate) *Query {
	q.having = append(q.having, predicates...)
	return q
}

// OrderBy appends sort expressions.
// Use criteria.Asc(field) and criteria.Desc(field) as constructors.
func (q *Query) OrderBy(orders ...Order) *Query {
	q.order = append(q.order, orders...)
	return q
}

// Limit sets the maximum number of rows to return (LIMIT n).
// Uses standard SQL syntax (PostgreSQL, MySQL).
// For Oracle / SQL Server, prefer manager.WithPage + PagedList().
func (q *Query) Limit(n int) *Query {
	q.limit = n
	return q
}

// Offset sets the number of rows to skip before returning results (OFFSET n).
func (q *Query) Offset(n int) *Query {
	q.offset = n
	return q
}

// Build

// Build compiles the full query (SELECT … ORDER BY … LIMIT … OFFSET …) into a
// SQL string and the corresponding bound parameters, using the provided dialect
// for placeholder format and LIKE behaviour.
//
// Pass the returned parameters directly to database/sql query functions.
func (q *Query) Build(d driver.FilterDialect) (string, []any) {
	b := newBuilder(d)
	var sb strings.Builder

	q.writeBody(&sb, b)
	q.writeOrderBy(&sb)
	q.writeLimitOffset(&sb)

	return sb.String(), b.params
}

// BuildBase compiles the query without ORDER BY, LIMIT, and OFFSET.
// Used internally by the manager when dialect-level pagination wraps the base query
// (manager.WithPage + PagedList). Prefer Build for direct execution.
func (q *Query) BuildBase(d driver.FilterDialect) (string, []any) {
	b := newBuilder(d)
	var sb strings.Builder

	q.writeBody(&sb, b)

	return sb.String(), b.params
}

// Internal helpers

// writeBody writes: SELECT … FROM … JOIN … WHERE … GROUP BY … HAVING …
func (q *Query) writeBody(sb *strings.Builder, b *builder) {
	// SELECT
	if len(q.selects) == 0 {
		sb.WriteString("SELECT *")
	} else {
		sb.WriteString("SELECT ")
		sb.WriteString(strings.Join(q.selects, ", "))
	}

	// FROM
	sb.WriteString(fmt.Sprintf(" FROM %s", q.table))
	if q.alias != "" {
		sb.WriteString(fmt.Sprintf(" %s", q.alias))
	}

	// JOINs
	for _, j := range q.joins {
		if j.alias != "" {
			sb.WriteString(fmt.Sprintf(" %s %s %s ON %s", j.joinType, j.table, j.alias, j.on))
		} else {
			sb.WriteString(fmt.Sprintf(" %s %s ON %s", j.joinType, j.table, j.on))
		}
	}

	// WHERE
	if len(q.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(b.buildClause(q.where))
	}

	// GROUP BY
	if len(q.group) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(q.group, ", "))
	}

	// HAVING
	if len(q.having) > 0 {
		sb.WriteString(" HAVING ")
		sb.WriteString(b.buildClause(q.having))
	}
}

func (q *Query) writeOrderBy(sb *strings.Builder) {
	if len(q.order) == 0 {
		return
	}
	parts := make([]string, len(q.order))
	for i, o := range q.order {
		parts[i] = fmt.Sprintf("%s %s", o.Field, o.Direction)
	}
	sb.WriteString(" ORDER BY ")
	sb.WriteString(strings.Join(parts, ", "))
}

func (q *Query) writeLimitOffset(sb *strings.Builder) {
	if q.limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", q.limit))
	}
	if q.offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", q.offset))
	}
}
