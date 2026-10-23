package docfields

import (
	"fmt"
	"slices"
	"testing"
)

// Most documents first, ties by name; a label no document holds and a count with no label behind
// it are not listed (Review Focus 1-3).
func TestListedNamesOrdersCapsAndDropsEmpty(t *testing.T) {
	labels := []Label{
		{ID: "l-urg", Kind: KindTag, Name: "Urgente"},
		{ID: "l-vuo", Kind: KindTag, Name: "Vuoto"},
		{ID: "l-boz", Kind: KindTag, Name: "bozza"},
		{ID: "l-int", Kind: KindTag, Name: "interno"},
	}
	counts := map[string]int{"l-int": 3, "l-boz": 1, "l-urg": 1, "l-gone": 5}
	names, omitted := listed(labels, counts)
	want := []ListedName{{Name: "interno", Documents: 3}, {Name: "bozza", Documents: 1}, {Name: "Urgente", Documents: 1}}
	if !slices.Equal(names, want) || omitted != 0 {
		t.Fatalf("listed %v, omitted %d; want %v, 0", names, omitted, want)
	}
}

// An empty library lists nothing, as an empty list the agent reads as "no tags" (Review Focus 4).
func TestListedNamesOfAnEmptyLibraryIsAnEmptyList(t *testing.T) {
	names, omitted := listed(nil, map[string]int{})
	if names == nil || len(names) != 0 || omitted != 0 {
		t.Fatalf("listed %#v, omitted %d", names, omitted)
	}
}

func TestListedNamesStopAtTheCapAndCountTheRest(t *testing.T) {
	var labels []Label
	counts := map[string]int{}
	for i := 1; i <= MaxListedNames+1; i++ {
		id := fmt.Sprintf("l-%02d", i)
		labels = append(labels, Label{ID: id, Kind: KindTag, Name: fmt.Sprintf("tag %02d", i)})
		counts[id] = i
	}
	names, omitted := listed(labels, counts)
	if len(names) != MaxListedNames || omitted != 1 {
		t.Fatalf("%d names, omitted %d", len(names), omitted)
	}
	if names[0] != (ListedName{Name: "tag 31", Documents: 31}) || names[MaxListedNames-1].Name != "tag 02" {
		t.Fatalf("first %v, last %v", names[0], names[MaxListedNames-1])
	}
}
