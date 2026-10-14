package property

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var testDict = &Dictionary{
	Types:      []Named{{"casa", "Casa"}, {"apartamento", "Apartamento"}, {"cobertura", "Cobertura"}, {"kitnet", "Kitnet"}, {"chacara", "Chácara"}, {"terreno", "Terreno"}},
	Offers:     []Named{{"venda", "Venda"}, {"aluguel", "Aluguel"}, {"lancamento", "Lançamento"}, {"leilao", "Leilão"}},
	Conditions: []Named{{"novo", "Novo"}, {"usado", "Usado"}},
	OwnerTypes: []Named{{"proprietario", "Proprietário"}, {"corretor", "Corretor"}, {"imobiliaria", "Imobiliária"}},
	Features:   []Named{{"piscina", "Piscina"}, {"vista_mar", "Vista para o mar"}, {"portaria_24h", "Portaria 24h"}, {"ar_condicionado", "Ar-condicionado"}, {"jardim", "Jardim"}},
	States:     []Named{{"PR", "Paraná"}, {"SP", "São Paulo"}, {"RJ", "Rio de Janeiro"}, {"SC", "Santa Catarina"}},
	Cities:     []City{{1, "Curitiba", "PR"}, {2, "São Paulo", "SP"}, {3, "Rio de Janeiro", "RJ"}, {4, "Florianópolis", "SC"}},
	Neighborhoods: []Neighborhood{{10, "Batel", 1, "Curitiba", "PR"}, {11, "Água Verde", 1, "Curitiba", "PR"},
		{12, "Centro", 1, "Curitiba", "PR"}, {13, "Centro", 4, "Florianópolis", "SC"}, {14, "Jardins", 2, "São Paulo", "SP"}},
}

func TestInterpretTheBriefExample(t *testing.T) {
	in := NewInterpreter(testDict).Interpret("Casa nova de 2 quartos, suíte, garagem 2 carros, curitiba, pr, bairro batel")
	assert.Equal(t, Filters{
		Types: []string{"casa"}, Conditions: []string{"novo"}, Bedrooms: 2, MinSuites: 1, MinParking: 2,
		CityIDs: []int64{1}, NeighborhoodIDs: []int64{10}, UFs: []string{"PR"},
	}, in.Filters)
	assert.Empty(t, in.Words, "everything was understood")
	assert.Empty(t, in.Text())
}

func TestInterpret(t *testing.T) {
	ip := NewInterpreter(testDict)
	tests := []struct {
		sentence string
		want     Filters
		words    []string
		features []string
	}{
		{"apartamento para alugar até 3.500 reais", Filters{Types: []string{"apartamento"}, Offers: []string{"aluguel"}, MaxPrice: 3500}, nil, nil},
		{"apto a venda a partir de 1,5 milhão", Filters{Types: []string{"apartamento"}, Offers: []string{"venda"}, MinPrice: 1.5e6}, nil, nil},
		{"casa até 900 mil", Filters{Types: []string{"casa"}, MaxPrice: 900e3}, nil, nil},
		{"R$ 1.200.000", Filters{MaxPrice: 1.2e6}, nil, nil},
		{"terreno 500m2", Filters{Types: []string{"terreno"}, MinArea: 500}, nil, nil},
		{"até 80 m²", Filters{MaxArea: 80}, nil, nil},
		{"pelo menos 3 quartos e 2 banheiros", Filters{MinBedrooms: 3, MinBathrooms: 2}, nil, nil},
		{"garagem para 2", Filters{MinParking: 2}, nil, nil},
		{"3 vagas de garagem", Filters{MinParking: 3}, nil, nil},
		{"imóvel novo na planta", Filters{Offers: []string{"lancamento"}}, nil, nil},
		{"direto com o dono", Filters{OwnerTypes: []string{"proprietario"}}, nil, nil},
		{"leilão rio de janeiro", Filters{Offers: []string{"leilao"}, CityIDs: []int64{3}}, nil, nil},
		{"são paulo", Filters{CityIDs: []int64{2}}, nil, nil},
		{"agua verde", Filters{NeighborhoodIDs: []int64{11}}, nil, nil},
		{"centro, florianópolis", Filters{CityIDs: []int64{4}, NeighborhoodIDs: []int64{13}}, nil, nil},
		{"cobertura com vista para o mar e piscina", Filters{Types: []string{"cobertura"}}, nil, []string{"Vista para o mar", "Piscina"}},
		{"portaria 24h, ar condicionado", Filters{}, nil, []string{"Portaria 24h", "Ar-condicionado"}},
		{"perto do parque barigui", Filters{}, []string{"parque", "barigui"}, nil},
		{"jardins", Filters{NeighborhoodIDs: []int64{14}}, nil, nil},
		{"casa com jardim", Filters{Types: []string{"casa"}}, nil, []string{"Jardim"}},
	}
	for _, tt := range tests {
		in := ip.Interpret(tt.sentence)
		assert.Equal(t, tt.want, in.Filters, tt.sentence)
		assert.Equal(t, tt.words, in.Words, tt.sentence)
		assert.Equal(t, tt.features, in.Features, tt.sentence)
	}
}

// A two-letter UF that is also a word counts only after a separator or in
// capitals; the others always count.
func TestInterpretUF(t *testing.T) {
	d := *testDict
	d.States = append(d.States, Named{"SE", "Sergipe"})
	ip := NewInterpreter(&d)
	assert.Equal(t, []string{"PR"}, ip.Interpret("casa pr").Filters.UFs)
	assert.Empty(t, ip.Interpret("casa se possível").Filters.UFs)
	assert.Equal(t, []string{"SE"}, ip.Interpret("aracaju, se").Filters.UFs)
	assert.Equal(t, []string{"SE"}, ip.Interpret("aracaju SE").Filters.UFs)
}

func TestInterpretChipsAndText(t *testing.T) {
	in := NewInterpreter(testDict).Interpret("casa nova 2 quartos com piscina até 900 mil no batel perto do shopping")
	labels := make([]string, len(in.Chips))
	for i, c := range in.Chips {
		labels[i] = c.Label
	}
	assert.Equal(t, []string{"Casa", "Novo", "2 quartos", "até R$ 900.000", "Batel · Curitiba/PR", "Piscina", "“shopping”"}, labels)
	assert.Equal(t, `shopping "Piscina"`, in.Text())

	in = NewInterpreter(testDict).Interpret("batel, curitiba, pr")
	assert.Len(t, in.Chips, 1, "the neighborhood names its city and state")
}

func TestTokenizeNumbers(t *testing.T) {
	for in, want := range map[string]float64{"2": 2, "2,5": 2.5, "1.500.000": 1.5e6, "1.500": 1500, "1.5": 1.5, "3.500,50": 3500.5} {
		assert.Equal(t, want, tokenize(in)[0].num, in)
	}
	toks := tokenize("100m² 24h")
	assert.Equal(t, []string{"100", "m2", "24", "h"}, []string{toks[0].key, toks[1].key, toks[2].key, toks[3].key})
}

func TestReplaceWords(t *testing.T) {
	assert.Equal(t, "Casa de 2 quartos, Curitiba", replaceWords("Casa de 2 qurtos, Curitba", []string{"qurtos", "Curitba"}, []string{"quartos", "Curitiba"}))
}

func TestFormat(t *testing.T) {
	assert.Equal(t, "R$ 1.250.000", formatMoney(1250000))
	assert.Equal(t, "R$ 950", formatMoney(950))
	assert.Equal(t, "2,5", formatNumber(2.5))
}
