package fts

import (
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// synonymMap reads Spec.Synonyms: each group lists equivalent words or
// phrases, and a searched word of a group also matches every other member.
// Only single words trigger an expansion; a phrase member is searched as a
// phrase.
func synonymMap(name string, groups [][]string) (map[string][][]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	m := map[string][][]string{}
	for _, g := range groups {
		members := make([][]string, 0, len(g))
		for _, entry := range g {
			words := splitWords(entry)
			if len(words) == 0 || len(words) > MaxTerms {
				return nil, fmt.Errorf("%w: %s: synonym %q needs 1 to %d words", ErrInvalidIndex, name, entry, MaxTerms)
			}
			members = append(members, words)
		}
		if len(members) < 2 {
			return nil, fmt.Errorf("%w: %s: a synonym group needs two members, got %q", ErrInvalidIndex, name, g)
		}
		for i, m1 := range members {
			if len(m1) != 1 {
				continue
			}
			for j, m2 := range members {
				if i != j && !hasWords(m[m1[0]], m2) {
					m[m1[0]] = append(m[m1[0]], m2)
				}
			}
		}
	}
	return m, nil
}

func hasWords(list [][]string, words []string) bool {
	for _, w := range list {
		if strings.Join(w, " ") == strings.Join(words, " ") {
			return true
		}
	}
	return false
}

// expand sets the synonyms of each single, non-prefix word of q.
func (i *Index) expand(q driver.TextQuery) {
	if len(i.synonyms) == 0 {
		return
	}
	for _, g := range q {
		for k, t := range g {
			if len(t.Words) == 1 && !t.Prefix {
				g[k].Synonyms = i.synonyms[t.Words[0]]
			}
		}
	}
}
