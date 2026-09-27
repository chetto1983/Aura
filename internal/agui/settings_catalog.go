package agui

// settings_catalog.go is the ONE shape every cockpit model picker is answered in. Image,
// video, cloud speech-to-text, text-to-speech and cloud embedding models come from two
// different upstream reads, both on OpenRouter whatever the chat route is, but the route
// contract is identical -- same envelope, same 503 when the catalogue is unwired, same 502
// carrying the upstream reason. One wrapper, so a picker added later inherits the contract
// instead of copying it.

import "net/http"

// catalogModelDTO is one picker row. A capability or price the catalog did not declare is
// omitted rather than zeroed: a present zero price means free, an absent one unknown.
type catalogModelDTO struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	ReferenceMax *int     `json:"reference_max,omitempty"`
	ImageMinUSD  *float64 `json:"image_min_usd,omitempty"`
	ImageMaxUSD  *float64 `json:"image_max_usd,omitempty"`
	// ImageTokenMinPer1M and ImageTokenMaxPer1M are the output-token rate of a model billed per
	// token, which has no per-image price. They are not counted in HasPrice, which keeps
	// meaning "a per-unit price the row can print as $/image or $/s".
	ImageTokenMinPer1M *float64 `json:"image_token_min_per_1m,omitempty"`
	ImageTokenMaxPer1M *float64 `json:"image_token_max_per_1m,omitempty"`
	DurationMin        *int     `json:"duration_min,omitempty"`
	DurationMax        *int     `json:"duration_max,omitempty"`
	Resolutions        []string `json:"resolutions,omitempty"`
	ImageToVideo       *bool    `json:"image_to_video,omitempty"`
	SecondMinUSD       *float64 `json:"second_min_usd,omitempty"`
	SecondMaxUSD       *float64 `json:"second_max_usd,omitempty"`
	Voices             []string `json:"voices,omitempty"`
	HasPrice           bool     `json:"has_price"`
}

type catalogDTO struct {
	Models []catalogModelDTO `json:"models"`
}

// writeCatalogRows answers one picker route. `wired` is whether the composition root
// supplied the catalogue; `name` appears in the 503 so an unwired route says WHICH
// catalogue is missing. `row` turns one upstream model into a picker row and reports
// whether to offer it: a model the picker could not produce a valid call for is dropped
// rather than listed, since offering it would only fail on selection.
func writeCatalogRows[M any](
	w http.ResponseWriter,
	wired bool,
	name string,
	list func() ([]M, error),
	row func(M) (catalogModelDTO, bool),
) {
	if !wired {
		writeJSONStatus(w, http.StatusServiceUnavailable,
			map[string]string{"error": name + " model catalog not configured"})
		return
	}
	models, err := list()
	if err != nil {
		// The catalog's reason names the failing read ("GET videos/models: 503"), which is what
		// tells the operator whether to wait or to look at the route.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := catalogDTO{Models: make([]catalogModelDTO, 0, len(models))}
	for _, model := range models {
		if dto, offer := row(model); offer {
			out.Models = append(out.Models, dto)
		}
	}
	writeJSON(w, out)
}
