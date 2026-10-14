package property

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Filters are the structured conditions read from a search sentence.
type Filters struct {
	Types, Offers, Conditions, OwnerTypes []string
	Bedrooms                              int // exact; 0 = any
	MinBedrooms, MinSuites                int
	MinBathrooms, MinParking              int
	MinArea, MaxArea                      float64
	MinPrice, MaxPrice                    float64
	CityIDs, NeighborhoodIDs              []int64
	UFs                                   []string
}

// Chip is one thing the interpreter understood, shown back to the user.
type Chip struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// Interpretation is a search sentence split into filters, the feature names
// it requires and the words left for the full-text search.
type Interpretation struct {
	Filters  Filters
	Features []string // feature names, searched as phrases
	Words    []string // free words, as typed
	Chips    []Chip
}

// Text is the full-text search of the interpretation: the free words and,
// as phrases, the required features.
func (in Interpretation) Text() string {
	parts := slices.Clone(in.Words)
	for _, f := range in.Features {
		parts = append(parts, `"`+f+`"`)
	}
	return strings.Join(parts, " ")
}

// HasFilters reports whether any structured condition was read.
func (in Interpretation) HasFilters() bool {
	f := in.Filters
	return len(f.Types)+len(f.Offers)+len(f.Conditions)+len(f.OwnerTypes)+len(f.CityIDs)+
		len(f.NeighborhoodIDs)+len(f.UFs) > 0 ||
		f.Bedrooms+f.MinBedrooms+f.MinSuites+f.MinBathrooms+f.MinParking > 0 ||
		f.MinArea+f.MaxArea+f.MinPrice+f.MaxPrice > 0
}

// Interpreter reads search sentences with the names of a Dictionary.
type Interpreter struct {
	phrases []phrase
	ufs     map[string]string // folded UF → UF
	dict    *Dictionary
}

type phraseKind uint8

// The order breaks ties between phrases of the same length: "sao paulo" is
// the city before the state, "jardins" the neighborhood before anything.
const (
	kindNeighborhood phraseKind = iota
	kindCity
	kindState
	kindFeature
	kindOffer
	kindType
	kindCondition
	kindOwner
)

type phrase struct {
	keys  []string
	kind  phraseKind
	codes []string // type, offer, condition, owner type, UF or feature code
	ids   []int64  // city or neighborhood ids sharing the name
}

// Aliases typed for the coded facets, besides their names.
var aliases = map[phraseKind]map[string][]string{
	kindType: {
		"casa": {"casa", "casas"}, "sobrado": {"sobrado", "sobrados"},
		"apartamento": {"apartamento", "apartamentos", "apto", "aptos", "ap"},
		"cobertura":   {"cobertura", "coberturas"}, "flat": {"flat", "flats"},
		"kitnet": {"kitnet", "kitnets", "kitinete", "quitinete", "kit"}, "studio": {"studio", "estudio"},
		"loft": {"loft"}, "chacara": {"chacara", "chacaras", "sitio"}, "terreno": {"terreno", "terrenos", "lote"},
	},
	kindOffer: {
		"aluguel":    {"alugar", "aluguel", "locacao", "alugo", "aluga", "para alugar", "pra alugar"},
		"venda":      {"comprar", "compra", "venda", "vender", "vende", "a venda"},
		"lancamento": {"lancamento", "lancamentos", "na planta", "imovel novo", "imoveis novos"},
		"leilao":     {"leilao", "leiloes", "leiloado"},
	},
	kindCondition: {
		"novo":  {"novo", "nova", "novos", "novas"},
		"usado": {"usado", "usada", "usados", "usadas", "velho", "velha", "antigo", "antiga"},
	},
	kindOwner: {
		"proprietario": {"proprietario", "dono", "particular", "direto com o dono", "direto com proprietario", "sem corretor"},
		"corretor":     {"corretor", "corretora", "corretores"},
		"imobiliaria":  {"imobiliaria", "imobiliarias"},
	},
	kindFeature: {
		"mobiliado":       {"mobiliada"},
		"pet":             {"pet", "aceita animais"},
		"vista_mar":       {"vista mar", "frente mar", "vista para o mar"},
		"ar_condicionado": {"ar condicionado"},
		"portaria_24h":    {"portaria", "portaria 24h", "portaria 24 horas"},
	},
}

// NewInterpreter builds the phrase table of d.
func NewInterpreter(d *Dictionary) *Interpreter {
	in := &Interpreter{ufs: map[string]string{}, dict: d}
	byKey := map[string]*phrase{}
	add := func(kind phraseKind, text, code string, id int64) {
		keys := keysOf(text)
		if len(keys) == 0 {
			return
		}
		k := fmt.Sprint(kind, strings.Join(keys, " "))
		p := byKey[k]
		if p == nil {
			p = &phrase{keys: keys, kind: kind}
			byKey[k] = p
		}
		if code != "" && !slices.Contains(p.codes, code) {
			p.codes = append(p.codes, code)
		}
		if id != 0 {
			p.ids = append(p.ids, id)
		}
	}
	for _, n := range d.Neighborhoods {
		add(kindNeighborhood, n.Name, "", n.ID)
	}
	for _, c := range d.Cities {
		add(kindCity, c.Name, "", c.ID)
	}
	for _, s := range d.States {
		add(kindState, s.Name, s.Code, 0)
		in.ufs[strings.ToLower(s.Code)] = s.Code
	}
	for kind, rows := range map[phraseKind][]Named{kindType: d.Types, kindOffer: d.Offers,
		kindCondition: d.Conditions, kindOwner: d.OwnerTypes, kindFeature: d.Features} {
		for _, r := range rows {
			add(kind, r.Name, r.Code, 0)
			for _, alias := range aliases[kind][r.Code] {
				add(kind, alias, r.Code, 0)
			}
		}
	}
	for _, p := range byKey {
		in.phrases = append(in.phrases, *p)
	}
	slices.SortFunc(in.phrases, func(a, b phrase) int {
		return cmp.Or(cmp.Compare(len(b.keys), len(a.keys)), cmp.Compare(a.kind, b.kind),
			strings.Compare(strings.Join(a.keys, " "), strings.Join(b.keys, " ")))
	})
	return in
}

// token is a word or a number of the sentence.
type token struct {
	raw      string
	key      string // lower case, without accents
	num      float64
	isNum    bool
	afterSep bool // follows , ; / - |: "curitiba, pr" separates the UF
	upper    bool // typed in capitals: "PR"
	used     bool
}

// tokenize splits s into words and numbers. "1.500.000" and "2,5" are
// numbers; "100m2", "100 m²" and "24h" split into a number and a unit.
func tokenize(s string) []token {
	var out []token
	rs := []rune(s)
	sep := false
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsDigit(r):
			j := i
			for j < len(rs) && (unicode.IsDigit(rs[j]) || (rs[j] == '.' || rs[j] == ',') && j+1 < len(rs) && unicode.IsDigit(rs[j+1])) {
				j++
			}
			raw := string(rs[i:j])
			out = append(out, token{raw: raw, key: raw, num: parseNumber(raw), isNum: true, afterSep: sep})
			sep = false
			i = j
		case unicode.IsLetter(r):
			j := i
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsMark(rs[j])) {
				j++
			}
			raw := string(rs[i:j])
			key := driver.FoldAccents(strings.ToLower(raw))
			if key == "m" && j < len(rs) && (rs[j] == '²' || rs[j] == '2') {
				key, raw = "m2", raw+string(rs[j])
				j++
			}
			out = append(out, token{raw: raw, key: key, afterSep: sep,
				upper: len(rs[i:j]) > 1 && strings.ToUpper(raw) == raw})
			sep = false
			i = j
		default:
			if strings.ContainsRune(",;/-|", r) {
				sep = true
			}
			i++
		}
	}
	return out
}

// parseNumber reads 2, 2,5 and 1.500.000 (Brazilian thousands) and 1.5.
func parseNumber(s string) float64 {
	switch {
	case strings.Contains(s, ","):
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	case strings.Count(s, ".") > 1 || strings.Contains(s, ".") && len(s)-strings.LastIndexByte(s, '.') == 4:
		s = strings.ReplaceAll(s, ".", "")
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func keysOf(text string) []string {
	var keys []string
	for _, t := range tokenize(text) {
		keys = append(keys, t.key)
	}
	return keys
}

type unit uint8

const (
	unitNone unit = iota
	unitBedroom
	unitSuite
	unitBathroom
	unitParking
	unitArea
	unitThousand
	unitMillion
)

var units = map[string]unit{
	"quarto": unitBedroom, "quartos": unitBedroom, "dorm": unitBedroom, "dorms": unitBedroom,
	"dormitorio": unitBedroom, "dormitorios": unitBedroom, "qto": unitBedroom, "qtos": unitBedroom,
	"suite": unitSuite, "suites": unitSuite,
	"banheiro": unitBathroom, "banheiros": unitBathroom, "wc": unitBathroom,
	"vaga": unitParking, "vagas": unitParking, "carro": unitParking, "carros": unitParking,
	"garagem": unitParking, "garagens": unitParking,
	"m2": unitArea, "m": unitArea, "metro": unitArea, "metros": unitArea,
	"mil": unitThousand, "k": unitThousand, "milhao": unitMillion, "milhoes": unitMillion, "mi": unitMillion,
}

var (
	minWords   = map[string]bool{"partir": true, "acima": true, "mais": true, "minimo": true, "min": true, "pelo": true}
	maxWords   = map[string]bool{"ate": true, "max": true, "maximo": true, "abaixo": true, "menos": true}
	moneyWords = map[string]bool{"r": true, "rs": true, "reais": true, "preco": true, "valor": true}
	// fillers are dropped: they carry no meaning for the search.
	fillers = map[string]bool{
		"de": true, "da": true, "do": true, "das": true, "dos": true, "com": true, "e": true, "em": true, "no": true,
		"na": true, "nos": true, "nas": true, "para": true, "pra": true, "p": true, "um": true, "uma": true, "o": true,
		"a": true, "os": true, "as": true, "que": true, "quero": true, "procuro": true, "busco": true, "preciso": true,
		"gostaria": true, "imovel": true, "imoveis": true, "bairro": true, "cidade": true, "estado": true,
		"regiao": true, "tenha": true, "tem": true, "ou": true, "quadrados": true, "por": true, "mes": true,
		"sendo": true, "pelo": true, "menos": true, "partir": true, "acima": true, "ate": true, "max": true,
		"maximo": true, "minimo": true, "min": true, "abaixo": true, "mais": true, "r": true, "rs": true,
		"reais": true, "preco": true, "valor": true, "h": true, "horas": true,
		"perto": true, "proximo": true, "proxima": true, "regioes": true,
	}
	// ambiguousUFs are also common words: a UF only after a separator or in capitals.
	ambiguousUFs = map[string]bool{"se": true, "es": true, "to": true, "ap": true, "ma": true, "al": true, "am": true, "pa": true}
)

// Interpret reads sentence.
func (ip *Interpreter) Interpret(sentence string) Interpretation {
	toks := tokenize(sentence)
	var in Interpretation
	ip.matchPhrases(toks, &in)
	matchNumbers(toks, &in)
	ip.matchRest(toks, &in)
	ip.restrictLocation(&in)
	in.Chips = ip.chips(in)
	return in
}

// matchPhrases consumes the longest known names first.
func (ip *Interpreter) matchPhrases(toks []token, in *Interpretation) {
	for i := range toks {
		for _, p := range ip.phrases {
			if i+len(p.keys) > len(toks) || !matches(toks[i:i+len(p.keys)], p.keys) {
				continue
			}
			for k := i; k < i+len(p.keys); k++ {
				toks[k].used = true
			}
			ip.apply(p, in)
			break
		}
	}
}

func matches(toks []token, keys []string) bool {
	for k, key := range keys {
		if toks[k].used || toks[k].key != key {
			return false
		}
	}
	return true
}

func (ip *Interpreter) apply(p phrase, in *Interpretation) {
	f := &in.Filters
	switch p.kind {
	case kindNeighborhood:
		f.NeighborhoodIDs = appendNew(f.NeighborhoodIDs, p.ids...)
	case kindCity:
		f.CityIDs = appendNew(f.CityIDs, p.ids...)
	case kindState:
		f.UFs = appendNew(f.UFs, p.codes...)
	case kindType:
		f.Types = appendNew(f.Types, p.codes...)
	case kindOffer:
		f.Offers = appendNew(f.Offers, p.codes...)
	case kindCondition:
		f.Conditions = appendNew(f.Conditions, p.codes...)
	case kindOwner:
		f.OwnerTypes = appendNew(f.OwnerTypes, p.codes...)
	case kindFeature:
		for _, code := range p.codes {
			in.Features = appendNew(in.Features, ip.name(ip.dict.Features, code))
		}
	}
}

// matchNumbers reads "2 quartos", "garagem para 2 carros", "100 m2",
// "até 900 mil", "a partir de R$ 3.000"; then "suíte" or "garagem" alone.
func matchNumbers(toks []token, in *Interpretation) {
	f := &in.Filters
	for i := range toks {
		t := &toks[i]
		if !t.isNum || t.used {
			continue
		}
		bound := qualifier(toks, i)
		u, at := unitAfter(toks, i)
		if u == unitNone {
			u, at = unitBefore(toks, i)
		}
		n := t.num
		switch u {
		case unitBedroom:
			if bound == "min" {
				f.MinBedrooms = int(n)
			} else {
				f.Bedrooms = int(n)
			}
		case unitSuite:
			f.MinSuites = int(n)
		case unitBathroom:
			f.MinBathrooms = int(n)
		case unitParking:
			f.MinParking = int(n)
		case unitArea:
			if bound == "max" {
				f.MaxArea = n
			} else {
				f.MinArea = n
			}
		case unitThousand, unitMillion:
			n *= map[unit]float64{unitThousand: 1e3, unitMillion: 1e6}[u]
			setPrice(f, n, bound)
		default:
			if !moneyAround(toks, i) && n < 10000 {
				continue
			}
			setPrice(f, n, bound)
		}
		t.used = true
		if at >= 0 {
			toks[at].used = true
		}
	}
	for i := range toks {
		t := &toks[i]
		if t.used {
			continue
		}
		switch units[t.key] {
		case unitSuite:
			f.MinSuites = max(f.MinSuites, 1)
			t.used = true
		case unitParking:
			f.MinParking = max(f.MinParking, 1)
			t.used = true
		}
	}
}

func setPrice(f *Filters, n float64, bound string) {
	if bound == "min" {
		f.MinPrice = n
	} else {
		f.MaxPrice = n
	}
}

// unitAfter is the unit right after the number ("2 quartos").
func unitAfter(toks []token, i int) (unit, int) {
	if i+1 < len(toks) && !toks[i+1].isNum && !toks[i+1].used {
		if u := units[toks[i+1].key]; u != unitNone {
			return u, i + 1
		}
	}
	return unitNone, -1
}

// unitBefore is the unit before the number, over connecting words
// ("garagem para 2", "quartos: 3"); the unit stays for "garagem" alone.
func unitBefore(toks []token, i int) (unit, int) {
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		if toks[j].isNum {
			break
		}
		if u := units[toks[j].key]; u != unitNone && u != unitThousand && u != unitMillion && u != unitArea {
			return u, j
		}
		if !fillers[toks[j].key] {
			break
		}
	}
	return unitNone, -1
}

// qualifier reads "até"/"a partir de" over up to three words before i.
func qualifier(toks []token, i int) string {
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		switch k := toks[j].key; {
		case k == "menos" && j > 0 && toks[j-1].key == "pelo": // "pelo menos"
			return "min"
		case minWords[k]:
			return "min"
		case maxWords[k]:
			return "max"
		case toks[j].isNum || !fillers[k]:
			return ""
		}
	}
	return ""
}

func moneyAround(toks []token, i int) bool {
	return i > 0 && moneyWords[toks[i-1].key] || i+1 < len(toks) && moneyWords[toks[i+1].key]
}

// matchRest reads the UFs and keeps the free words.
func (ip *Interpreter) matchRest(toks []token, in *Interpretation) {
	for i := range toks {
		t := &toks[i]
		if t.used || t.isNum {
			continue
		}
		if uf, ok := ip.ufs[t.key]; ok && (!ambiguousUFs[t.key] || t.afterSep || t.upper) {
			in.Filters.UFs = appendNew(in.Filters.UFs, uf)
			t.used = true
			continue
		}
		if fillers[t.key] || len([]rune(t.key)) < 2 {
			continue
		}
		in.Words = append(in.Words, t.raw)
	}
}

// restrictLocation keeps the neighborhoods of the cities and states typed:
// "centro, curitiba" is the Centro of Curitiba only.
func (ip *Interpreter) restrictLocation(in *Interpretation) {
	f := &in.Filters
	if len(f.NeighborhoodIDs) == 0 || len(f.CityIDs)+len(f.UFs) == 0 {
		return
	}
	var kept []int64
	for _, id := range f.NeighborhoodIDs {
		n := ip.neighborhood(id)
		if (len(f.CityIDs) == 0 || slices.Contains(f.CityIDs, n.CityID)) && (len(f.UFs) == 0 || slices.Contains(f.UFs, n.UF)) {
			kept = append(kept, id)
		}
	}
	if len(kept) > 0 {
		f.NeighborhoodIDs = kept
	}
}

func (ip *Interpreter) neighborhood(id int64) Neighborhood {
	for _, n := range ip.dict.Neighborhoods {
		if n.ID == id {
			return n
		}
	}
	return Neighborhood{}
}

func (ip *Interpreter) name(rows []Named, code string) string {
	for _, r := range rows {
		if r.Code == code {
			return r.Name
		}
	}
	return code
}

// chips describes what was understood, in reading order of importance.
func (ip *Interpreter) chips(in Interpretation) []Chip {
	f := in.Filters
	var out []Chip
	add := func(kind, label string) { out = append(out, Chip{Kind: kind, Label: label}) }
	for _, c := range f.Types {
		add("tipo", ip.name(ip.dict.Types, c))
	}
	for _, c := range f.Conditions {
		add("condicao", ip.name(ip.dict.Conditions, c))
	}
	for _, c := range f.Offers {
		add("oferta", ip.name(ip.dict.Offers, c))
	}
	for _, c := range f.OwnerTypes {
		add("anunciante", ip.name(ip.dict.OwnerTypes, c))
	}
	if f.Bedrooms > 0 {
		add("quartos", plural(f.Bedrooms, "quarto", "quartos"))
	}
	if f.MinBedrooms > 0 {
		add("quartos", fmt.Sprintf("%d+ quartos", f.MinBedrooms))
	}
	if f.MinSuites > 0 {
		add("suites", fmt.Sprintf("%d+ %s", f.MinSuites, pick(f.MinSuites, "suíte", "suítes")))
	}
	if f.MinBathrooms > 0 {
		add("banheiros", fmt.Sprintf("%d+ %s", f.MinBathrooms, pick(f.MinBathrooms, "banheiro", "banheiros")))
	}
	if f.MinParking > 0 {
		add("vagas", fmt.Sprintf("%d+ %s", f.MinParking, pick(f.MinParking, "vaga", "vagas")))
	}
	if f.MinArea > 0 {
		add("area", fmt.Sprintf("a partir de %s m²", formatNumber(f.MinArea)))
	}
	if f.MaxArea > 0 {
		add("area", fmt.Sprintf("até %s m²", formatNumber(f.MaxArea)))
	}
	if f.MinPrice > 0 {
		add("preco", "a partir de "+formatMoney(f.MinPrice))
	}
	if f.MaxPrice > 0 {
		add("preco", "até "+formatMoney(f.MaxPrice))
	}
	// A neighborhood already names its city and state: those chips repeat it.
	shownCities, shownUFs := map[int64]bool{}, map[string]bool{}
	for _, id := range f.NeighborhoodIDs {
		n := ip.neighborhood(id)
		add("bairro", n.Name+" · "+n.City+"/"+n.UF)
		shownCities[n.CityID], shownUFs[n.UF] = true, true
	}
	for _, id := range f.CityIDs {
		for _, c := range ip.dict.Cities {
			if c.ID == id && !shownCities[id] {
				add("cidade", c.Name+"/"+c.UF)
				shownUFs[c.UF] = true
			}
		}
	}
	for _, uf := range f.UFs {
		if !shownUFs[uf] {
			add("estado", uf)
		}
	}
	for _, name := range in.Features {
		add("caracteristica", name)
	}
	if len(in.Words) > 0 {
		add("texto", "“"+strings.Join(in.Words, " ")+"”")
	}
	return out
}

func appendNew[T comparable](s []T, vs ...T) []T {
	for _, v := range vs {
		if !slices.Contains(s, v) {
			s = append(s, v)
		}
	}
	return s
}

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int, one, many string) string { return fmt.Sprintf("%d %s", n, pick(n, one, many)) }
