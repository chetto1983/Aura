package mediagen

// ImageInput is the caller-requested shape of an image generation call, before
// ClampImage narrows it to what the target Model actually declares.
type ImageInput struct {
	Prompt            string
	AspectRatio       string
	ReferenceAssetIDs []string
}

// VideoInput is the caller-requested shape of a video generation call, before
// ClampVideo narrows it to what the target Model actually declares.
type VideoInput struct {
	Prompt            string
	Duration          int
	Resolution        string
	AspectRatio       string
	FirstFrameAssetID string
	ReferenceAssetIDs []string
	Audio             *bool
}

// Parameter describes one capability a catalog Model declares: either an
// enumerated set of Values (e.g. resolutions expressed as strings), or a
// numeric range via Min/Max. A nil Min or Max means that bound is undeclared.
type Parameter struct {
	Type   string
	Values []string
	Min    *int
	Max    *int
}

// PriceLine is one billable/unit/variant price row as OpenRouter's endpoint
// pricing describes it, carrying a USD cost whose meaning depends on Unit.
type PriceLine struct {
	Billable string
	Unit     string
	Variant  string
	CostUSD  float64
}

// Model is one catalog entry normalized from OpenRouter's image/video model
// list plus (best-effort) its endpoint pricing detail. Parameters, Durations,
// Resolutions, AspectRatios and FrameImages are the provider's declared
// capabilities; a nil/empty entry means the provider declared nothing, and no
// default is ever invented for it. ImagePricing is nil when enrichment failed:
// the price is unknown, never free.
type Model struct {
	ID            string
	Kind          Kind
	Parameters    map[string]Parameter
	Durations     []int
	Resolutions   []string
	AspectRatios  []string
	FrameImages   []string
	GenerateAudio bool
	PricingSKUs   map[string]string
	ImagePricing  []PriceLine
}
