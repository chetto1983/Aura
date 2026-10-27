package obs

// The catalog's own contract checks: the golden manifest, the invariants and the attribute
// vocabulary. Only this package's tests read them; the running binary trusts the catalog
// these tests pin.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Catalog returns a deep copy of the stable metric descriptor catalog.
func Catalog() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	for i, descriptor := range descriptors {
		out[i] = descriptor
		out[i].Attributes = append([]AttributeKey(nil), descriptor.Attributes...)
		out[i].Buckets = append([]float64(nil), descriptor.Buckets...)
	}
	return out
}

// CatalogManifest renders the deterministic verifier/dashboard contract.
func CatalogManifest() string {
	var out strings.Builder
	for _, descriptor := range descriptors {
		attrs := make([]string, len(descriptor.Attributes))
		for i, key := range descriptor.Attributes {
			attrs[i] = string(key)
		}
		buckets := make([]string, len(descriptor.Buckets))
		for i, boundary := range descriptor.Buckets {
			buckets[i] = strconv.FormatFloat(boundary, 'g', -1, 64)
		}
		_, _ = fmt.Fprintf(&out, "%s|%s|%s|%s|%s|%s|%s|%s\n",
			descriptor.ID, descriptor.Name, descriptor.PrometheusName, descriptor.Kind,
			descriptor.Unit, strings.Join(attrs, ","), descriptor.Description, strings.Join(buckets, ","))
	}
	return out.String()
}

// ValidateCatalog checks ownership, naming, attribute, and bucket invariants.
func ValidateCatalog() error {
	seenIDs := map[InstrumentID]bool{}
	seenNames := map[string]bool{}
	seenProm := map[string]bool{}
	for _, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.Name == "" || descriptor.PrometheusName == "" || descriptor.Description == "" {
			return fmt.Errorf("incomplete metric descriptor: %+v", descriptor)
		}
		if seenIDs[descriptor.ID] || seenNames[descriptor.Name] || seenProm[descriptor.PrometheusName] {
			return fmt.Errorf("duplicate metric descriptor owner: %s", descriptor.ID)
		}
		seenIDs[descriptor.ID], seenNames[descriptor.Name], seenProm[descriptor.PrometheusName] = true, true, true
		if !strings.HasPrefix(descriptor.Name, "aura.") || !strings.HasPrefix(descriptor.PrometheusName, "aura_") {
			return fmt.Errorf("metric %s has non-Aura name", descriptor.ID)
		}
		for _, key := range descriptor.Attributes {
			if !IsBoundedAttribute(key) {
				return fmt.Errorf("metric %s has unbounded attribute %q", descriptor.ID, key)
			}
		}
		if descriptor.Kind == KindHistogram {
			if len(descriptor.Buckets) == 0 || !sort.Float64sAreSorted(descriptor.Buckets) {
				return fmt.Errorf("histogram %s has invalid buckets", descriptor.ID)
			}
			for i := 1; i < len(descriptor.Buckets); i++ {
				if descriptor.Buckets[i] <= descriptor.Buckets[i-1] {
					return fmt.Errorf("histogram %s buckets are not strictly increasing", descriptor.ID)
				}
			}
		} else if len(descriptor.Buckets) != 0 {
			return fmt.Errorf("non-histogram %s has buckets", descriptor.ID)
		}
	}
	return nil
}

// AllAttributeKeys returns the complete finite metric-dimension vocabulary.
func AllAttributeKeys() []AttributeKey {
	return []AttributeKey{AttributeOperation, AttributeToolClass, AttributeTransport, AttributeOutcome, AttributeErrorClass, AttributeState}
}
