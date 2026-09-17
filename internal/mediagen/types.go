package mediagen

// Status is a video generation job's lifecycle state, as OpenRouter's video
// API reports it (submit response and poll response share the same enum).
type Status string

// The six job statuses OpenRouter's video API declares.
const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusExpired    Status = "expired"
	StatusCancelled  Status = "cancelled"
)

// VideoExtension names a clip of mimeType for storage and delivery. Aura keeps video only as MP4
// or WebM, the two formats the assets service accepts as video; any other type is refused as
// unsupported.
func VideoExtension(mimeType string) (string, error) {
	switch mimeType {
	case "video/mp4":
		return ".mp4", nil
	case "video/webm":
		return ".webm", nil
	}
	return "", &Error{Code: "unsupported", Message: "The generated video is neither MP4 nor WebM."}
}

// ImageExtension names a generated image of mimeType for storage and delivery: the four types
// resolveImageMIME can return. The table is fixed rather than mime.ExtensionsByType, whose
// answer depends on the host — Windows lists .jfif first for image/jpeg — while the extension
// has to name the same type again when the stored file is typed back from its name. Any other
// type is refused as unsupported; it holds images only, so an image path can never name a clip.
func ImageExtension(mimeType string) (string, error) {
	switch mimeType {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	case "image/svg+xml":
		return ".svg", nil
	}
	return "", &Error{Code: "unsupported", Message: "The generated image is neither PNG, JPEG, WebP nor SVG."}
}

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
	LastFrameAssetID  string
	ReferenceAssetIDs []string
	Audio             *bool
	Seed              *int
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
	ID   string
	Kind Kind
	// Name and Description are as the provider names it; empty when it declares none.
	Name          string
	Description   string
	Parameters    map[string]Parameter
	Durations     []int
	Resolutions   []string
	AspectRatios  []string
	FrameImages   []string
	GenerateAudio bool
	// Seed reports whether the provider accepts a caller-supplied seed for reproducibility.
	Seed         bool
	PricingSKUs  map[string]string
	ImagePricing []PriceLine
}
