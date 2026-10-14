package property

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
)

// PageSize is the number of properties per page.
const PageSize = 10

// Result is one property found.
type Result struct {
	ID           int64   `db:"id" json:"id"`
	Title        string  `db:"title" json:"title"`
	Street       string  `db:"street" json:"street"`
	Bedrooms     int     `db:"bedrooms" json:"bedrooms"`
	Suites       int     `db:"suites" json:"suites"`
	Bathrooms    int     `db:"bathrooms" json:"bathrooms"`
	Parking      int     `db:"parking_spaces" json:"parking"`
	Area         float64 `db:"area_m2" json:"area"`
	Price        float64 `db:"price" json:"price"`
	Type         string  `db:"property_type" json:"type"`
	Offer        string  `db:"offer" json:"offer"`
	OfferCode    string  `db:"offer_code" json:"offerCode"`
	Condition    string  `db:"condition" json:"condition"`
	Owner        string  `db:"owner" json:"owner"`
	OwnerType    string  `db:"owner_type" json:"ownerType"`
	Neighborhood string  `db:"neighborhood" json:"neighborhood"`
	City         string  `db:"city" json:"city"`
	UF           string  `db:"uf" json:"uf"`
	FeatureList  string  `db:"features" json:"-"`
	Relevance    float64 `db:"relevance" json:"relevance"`
	// The title and description with the matched words marked; rendered by
	// TitleHTML and SnippetHTML, which escape the text.
	TitleMarked       fts.Highlight `db:"title_hl" json:"-"`
	DescriptionMarked fts.Highlight `db:"description_hl" json:"-"`

	TitleHTML   string   `json:"titleHtml"`
	SnippetHTML string   `json:"snippetHtml"`
	PriceLabel  string   `json:"priceLabel"`
	Features    []string `json:"features"`
}

// Response is a page of results and what the sentence was understood as.
type Response struct {
	Query       string   `json:"query"`
	Corrected   string   `json:"corrected,omitempty"` // "você quis dizer"
	Understood  []Chip   `json:"understood"`
	Approximate string   `json:"approximate,omitempty"` // how the search was relaxed
	Total       uint64   `json:"total"`
	Page        uint64   `json:"page"`
	Pages       uint64   `json:"pages"`
	Results     []Result `json:"results"`
}

// Search reads sentence, corrects its misspellings, and searches with the
// filters and words it holds. When nothing matches, it relaxes the search:
// any of the words instead of all of them, then the filters alone.
func (s *Service) Search(ctx context.Context, sentence string, page uint16) (*Response, error) {
	ip := s.interpreter()
	in := ip.Interpret(sentence)
	resp := &Response{Query: sentence}

	// Words the interpreter did not know may be misspelled names it does:
	// the lexicon corrects them, and the corrected sentence is read again.
	if words := correctable(in.Words); len(words) > 0 {
		fixed, changed, err := sqln.SuggestSearch(ctx, searchIndex.Search(strings.Join(words, " ")).Mode(fts.AllTerms))
		if err != nil {
			return nil, err
		}
		if changed {
			resp.Corrected = replaceWords(sentence, words, strings.Fields(fixed.Term()))
			in = ip.Interpret(resp.Corrected)
		}
	}
	resp.Understood = in.Chips

	// Relax the search until something matches, least important first;
	// type, offer and place are what the person wants most and stay.
	plan := searchPlan{filters: in.Filters, words: in.Words, features: in.Features, mode: fts.Websearch}
	var relaxed []string
	for _, step := range append([]relaxation{{}}, ip.relaxations(in.HasFilters())...) {
		if step.apply != nil {
			if !step.apply(&plan) {
				continue
			}
			relaxed = append(relaxed, step.label)
		}
		res, err := s.run(ctx, plan, page)
		if err != nil {
			return nil, err
		}
		resp.Total, resp.Page, resp.Pages, resp.Results = res.TotalElements, res.Number, res.TotalPages, res.Content
		if res.TotalElements > 0 {
			if len(relaxed) > 0 {
				resp.Approximate = "Nenhum imóvel atende a tudo o que foi pedido. Mostrando os mais próximos: " + strings.Join(relaxed, ", ") + "."
			}
			break
		}
	}
	for i := range resp.Results {
		r := &resp.Results[i]
		r.TitleHTML = r.TitleMarked.HTML()
		r.SnippetHTML = r.DescriptionMarked.Excerpt(42).HTML()
		r.PriceLabel = formatMoney(r.Price)
		if r.OfferCode == "aluguel" {
			r.PriceLabel += "/mês"
		}
		r.Features = []string{}
		if r.FeatureList != "" {
			r.Features = strings.Split(r.FeatureList, "|")
		}
	}
	if resp.Results == nil {
		resp.Results = []Result{}
	}
	return resp, nil
}

// searchPlan is one attempt: filters and the full-text search, if any.
type searchPlan struct {
	filters  Filters
	words    []string // free words
	features []string // feature names, searched as phrases
	mode     fts.Mode
}

func (p searchPlan) text() string {
	return Interpretation{Words: p.words, Features: p.features}.Text()
}

// relaxation loosens a plan; apply reports whether it changed anything.
type relaxation struct {
	label string
	apply func(*searchPlan) bool
}

func (ip *Interpreter) relaxations(hasFilters bool) []relaxation {
	return []relaxation{
		// Features are wishes; free words are usually the most specific part
		// (a place, a landmark): features go first.
		{"sem exigir as características", func(p *searchPlan) bool {
			// With features only, "any of them" (next step) keeps partial matches.
			if len(p.features) == 0 || len(p.words) == 0 {
				return false
			}
			p.features = nil
			return true
		}},
		{"com só algumas das palavras", func(p *searchPlan) bool {
			if p.text() == "" || p.mode == fts.AnyTerm {
				return false
			}
			p.mode = fts.AnyTerm
			return true
		}},
		{"sem as palavras", func(p *searchPlan) bool {
			if p.text() == "" || !hasFilters {
				return false
			}
			p.words, p.features = nil, nil
			return true
		}},
		{"sem limite de preço e área", func(p *searchPlan) bool {
			f := &p.filters
			if f.MinPrice+f.MaxPrice+f.MinArea+f.MaxArea == 0 {
				return false
			}
			f.MinPrice, f.MaxPrice, f.MinArea, f.MaxArea = 0, 0, 0, 0
			return true
		}},
		{"com mais quartos também", func(p *searchPlan) bool {
			f := &p.filters
			if f.Bedrooms == 0 {
				return false
			}
			f.MinBedrooms, f.Bedrooms = f.Bedrooms, 0
			return true
		}},
		{"sem número de quartos, suítes, banheiros e vagas", func(p *searchPlan) bool {
			f := &p.filters
			if f.MinBedrooms+f.MinSuites+f.MinBathrooms+f.MinParking == 0 {
				return false
			}
			f.MinBedrooms, f.MinSuites, f.MinBathrooms, f.MinParking = 0, 0, 0, 0
			return true
		}},
		{"novos e usados, de qualquer anunciante", func(p *searchPlan) bool {
			f := &p.filters
			if len(f.Conditions)+len(f.OwnerTypes) == 0 {
				return false
			}
			f.Conditions, f.OwnerTypes = nil, nil
			return true
		}},
		{"em toda a cidade", func(p *searchPlan) bool {
			f := &p.filters
			if len(f.NeighborhoodIDs) == 0 {
				return false
			}
			for _, id := range f.NeighborhoodIDs {
				f.CityIDs = appendNew(f.CityIDs, ip.neighborhood(id).CityID)
			}
			f.NeighborhoodIDs = nil
			return true
		}},
	}
}

// run searches with the plan; a text of stopwords only searches by the
// filters alone.
func (s *Service) run(ctx context.Context, p searchPlan, page uint16) (*sqln.Page[Result], error) {
	if p.text() == "" {
		return s.query(ctx, p.filters, nil, page)
	}
	search := searchIndex.Search(p.text()).Mode(p.mode)
	res, err := s.query(ctx, p.filters, &search, page)
	if errors.Is(err, fts.ErrEmptySearch) {
		return s.query(ctx, p.filters, nil, page)
	}
	return res, err
}

// query runs one search: the filters, and the full-text search when set.
func (s *Service) query(ctx context.Context, f Filters, search *fts.Search, page uint16) (*sqln.Page[Result], error) {
	q := criteria.From("properties", "p").
		Select("p.id", "p.title", "p.street", "p.bedrooms", "p.suites", "p.bathrooms", "p.parking_spaces",
			"p.area_m2", "p.price", "t.name AS property_type", "o.name AS offer", "o.code AS offer_code",
			"cd.name AS condition", "ow.name AS owner", "ot.name AS owner_type", "n.name AS neighborhood",
			"c.name AS city", "c.uf AS uf",
			`COALESCE((SELECT string_agg(f.name, '|' ORDER BY f.name) FROM property_features pf
			  JOIN features f ON f.code = pf.feature WHERE pf.property_id = p.id), '') AS features`).
		Join("neighborhoods", "n", "n.id = p.neighborhood_id").
		Join("cities", "c", "c.id = n.city_id").
		Join("property_types", "t", "t.code = p.property_type").
		Join("offer_types", "o", "o.code = p.offer_type").
		Join("conditions", "cd", "cd.code = p.condition").
		Join("owners", "ow", "ow.id = p.owner_id").
		Join("owner_types", "ot", "ot.code = ow.owner_type")

	where := []criteria.Predicate{criteria.IsTrue("p.active")}
	in := func(field string, values []string) {
		if len(values) > 0 {
			where = append(where, criteria.In(field, values))
		}
	}
	in("p.property_type", f.Types)
	in("p.offer_type", f.Offers)
	in("p.condition", f.Conditions)
	in("ow.owner_type", f.OwnerTypes)
	in("c.uf", f.UFs)
	if len(f.NeighborhoodIDs) > 0 {
		where = append(where, criteria.In("n.id", f.NeighborhoodIDs))
	} else if len(f.CityIDs) > 0 {
		where = append(where, criteria.In("c.id", f.CityIDs))
	}
	atLeast := func(field string, n float64) {
		if n > 0 {
			where = append(where, criteria.Gte(field, n))
		}
	}
	if f.Bedrooms > 0 {
		where = append(where, criteria.Eq("p.bedrooms", f.Bedrooms))
	}
	atLeast("p.bedrooms", float64(f.MinBedrooms))
	atLeast("p.suites", float64(f.MinSuites))
	atLeast("p.bathrooms", float64(f.MinBathrooms))
	atLeast("p.parking_spaces", float64(f.MinParking))
	atLeast("p.area_m2", f.MinArea)
	atLeast("p.price", f.MinPrice)
	if f.MaxArea > 0 {
		where = append(where, criteria.Lte("p.area_m2", f.MaxArea))
	}
	if f.MaxPrice > 0 {
		where = append(where, criteria.Lte("p.price", f.MaxPrice))
	}

	order := []pagination.Sort{pagination.NewSort("p.created_at", pagination.DESC)}
	if search != nil {
		q.SelectRelevance(*search, "relevance").
			SelectHighlight(*search, "title", "title_hl").
			SelectHighlight(*search, "description", "description_hl")
		where = append(where, criteria.Match(*search))
		order = []pagination.Sort{pagination.NewSort("relevance", pagination.DESC), pagination.NewSort("p.id", pagination.ASC)}
	} else {
		q.Select("0::float8 AS relevance", "p.title AS title_hl", "p.description AS description_hl")
	}
	return sqln.FindFromCriteria[Result](ctx, q.Where(where...)).
		WithPage(pagination.NewPageRequest(page, PageSize, order)).PagedList()
}

// correctable are the free words worth checking for typos: 3 letters or more.
func correctable(words []string) []string {
	var out []string
	for _, w := range words {
		if len([]rune(w)) >= 3 && strings.IndexFunc(w, unicode.IsDigit) < 0 {
			out = append(out, w)
		}
	}
	return out
}

// replaceWords rewrites in sentence each word of from with the word of to at
// the same position, comparing without case and accents.
func replaceWords(sentence string, from, to []string) string {
	if len(from) != len(to) {
		return sentence
	}
	fix := map[string]string{}
	for i := range from {
		if !strings.EqualFold(from[i], to[i]) {
			fix[driver.FoldAccents(strings.ToLower(from[i]))] = to[i]
		}
	}
	var b strings.Builder
	rs := []rune(sentence)
	for i := 0; i < len(rs); {
		if !unicode.IsLetter(rs[i]) {
			b.WriteRune(rs[i])
			i++
			continue
		}
		j := i
		for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsMark(rs[j])) {
			j++
		}
		w := string(rs[i:j])
		if r, ok := fix[driver.FoldAccents(strings.ToLower(w))]; ok {
			w = r
		}
		b.WriteString(w)
		i = j
	}
	return b.String()
}
