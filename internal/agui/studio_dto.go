package agui

// studio_dto.go is the Studio's wire contract: the two bodies the composer posts, the model
// rows its pickers read, the record rows its history reads, and the one place a refusal
// becomes a status code. The cockpit reads nothing else about a generation, so a field that
// is not here is a field the Studio cannot show.

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

// StudioVideoRequest is POST /api/studio/videos' body.
type StudioVideoRequest struct {
	Model             string `json:"model"`
	Prompt            string `json:"prompt"`
	Duration          int    `json:"duration"`
	Resolution        string `json:"resolution"`
	AspectRatio       string `json:"aspect_ratio"`
	Audio             *bool  `json:"audio"`
	Seed              *int   `json:"seed"`
	FirstFrameAssetID string `json:"first_frame_asset_id"`
	LastFrameAssetID  string `json:"last_frame_asset_id"`
}

// StudioImageRequest is POST /api/studio/images' body.
type StudioImageRequest struct {
	Model             string   `json:"model"`
	Prompt            string   `json:"prompt"`
	AspectRatio       string   `json:"aspect_ratio"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
}

// studioPriceDTO is one cell of a video model's price matrix: what a second costs at this
// resolution with this audio choice. The composer prices the clip the operator is about to
// pay for, so an estimate built from a single rate would be wrong for every other cell.
type studioPriceDTO struct {
	Resolution   string  `json:"resolution"`
	Audio        bool    `json:"audio"`
	USDPerSecond float64 `json:"usd_per_second"`
}

// studioModelDTO is one picker row. A capability or price the catalog did not declare is
// omitted rather than zeroed: an absent price is unknown, never free.
type studioModelDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`

	Durations    []int            `json:"durations,omitempty"`
	Resolutions  []string         `json:"resolutions,omitempty"`
	AspectRatios []string         `json:"aspect_ratios,omitempty"`
	FrameImages  []string         `json:"frame_images,omitempty"`
	Audio        bool             `json:"audio"`
	Seed         bool             `json:"seed"`
	Prices       []studioPriceDTO `json:"prices,omitempty"`

	ReferenceMax       *int     `json:"reference_max,omitempty"`
	ImageMinUSD        *float64 `json:"image_min_usd,omitempty"`
	ImageMaxUSD        *float64 `json:"image_max_usd,omitempty"`
	ImageTokenMinPer1M *float64 `json:"image_token_min_per_1m,omitempty"`
	ImageTokenMaxPer1M *float64 `json:"image_token_max_per_1m,omitempty"`
}

type studioModelsDTO struct {
	Default string           `json:"default"`
	Models  []studioModelDTO `json:"models"`
}

// studioUsedDTO is what the provider was actually asked for, after the clamp: the composer
// shows it beside the prompt so a narrowed request is visible rather than silently different.
type studioUsedDTO struct {
	Duration          int      `json:"duration,omitempty"`
	Resolution        string   `json:"resolution,omitempty"`
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	Audio             *bool    `json:"audio,omitempty"`
	Seed              *int     `json:"seed,omitempty"`
	FirstFrameAssetID string   `json:"first_frame_asset_id,omitempty"`
	LastFrameAssetID  string   `json:"last_frame_asset_id,omitempty"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
}

// studioRecordDTO is one history row, and the answer to both create routes.
type studioRecordDTO struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Status      string          `json:"status"`
	Model       string          `json:"model"`
	Prompt      string          `json:"prompt"`
	Used        studioUsedDTO   `json:"used"`
	Adjustments []string        `json:"adjustments,omitempty"`
	CostUSD     *float64        `json:"cost_usd,omitempty"`
	AssetID     string          `json:"asset_id,omitempty"`
	Error       *mediagen.Error `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type studioHistoryDTO struct {
	Records []studioRecordDTO `json:"records"`
}

// studioAssetDTO is one library row. The object bucket, key and ETag are the storage layout,
// which the browser never needs and must not learn: every read goes through an owned,
// identity-scoped asset route.
type studioAssetDTO struct {
	ID        string    `json:"id"`
	Modality  string    `json:"modality"`
	Status    string    `json:"status"`
	FileName  string    `json:"file_name"`
	MIMEType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type studioLibraryDTO struct {
	Assets []studioAssetDTO `json:"assets"`
}

func studioAsset(asset assets.Asset) studioAssetDTO {
	return studioAssetDTO{
		ID: asset.ID, Modality: string(asset.Modality), Status: string(asset.Status),
		FileName: asset.FileName, MIMEType: asset.MIMEType, SizeBytes: asset.SizeBytes,
		CreatedAt: asset.CreatedAt,
	}
}

// studioModel narrows a catalog entry to the picker row of its kind. The video branch prices
// the whole matrix rather than a range, because the composer has to name one cell of it.
func studioModel(model mediagen.Model, kind mediagen.Kind) studioModelDTO {
	row := studioModelDTO{ID: model.ID, Name: model.Name, Description: model.Description, AspectRatios: model.AspectRatios}
	if kind == mediagen.KindImage {
		if references, declared := model.Parameters["input_references"]; declared && references.Max != nil {
			row.ReferenceMax = new(max(*references.Max, 0))
		}
		if low, high, ok := mediagen.ImagePrice(model.ImagePricing); ok {
			row.ImageMinUSD, row.ImageMaxUSD = &low, &high
		}
		if low, high, ok := mediagen.ImageTokenPricePerMillion(model.ImagePricing); ok {
			row.ImageTokenMinPer1M, row.ImageTokenMaxPer1M = &low, &high
		}
		return row
	}
	row.Durations, row.Resolutions, row.FrameImages = model.Durations, model.Resolutions, model.FrameImages
	row.Audio, row.Seed = model.GenerateAudio, model.Seed
	row.Prices = studioVideoPrices(model)
	return row
}

// studioVideoPrices is the per-second price of every clip this model can be asked for: each
// declared resolution — or the single undeclared one — against each audio choice it allows.
func studioVideoPrices(model mediagen.Model) []studioPriceDTO {
	resolutions := model.Resolutions
	if len(resolutions) == 0 {
		resolutions = []string{""}
	}
	audioChoices := []bool{false}
	if model.GenerateAudio {
		audioChoices = append(audioChoices, true)
	}
	prices := make([]studioPriceDTO, 0, len(resolutions)*len(audioChoices))
	for _, resolution := range resolutions {
		for _, audio := range audioChoices {
			usd, ok := mediagen.VideoSecondPrice(model.PricingSKUs, resolution, audio)
			if !ok {
				continue
			}
			prices = append(prices, studioPriceDTO{Resolution: resolution, Audio: audio, USDPerSecond: usd})
		}
	}
	return prices
}

// studioRecord reads a persisted job back into its history row. A request that cannot be
// decoded is a row whose prompt, clamp and notes are lost — the row itself is still answered,
// because its status and asset are what the Studio needs to show the result.
func studioRecord(job mediagen.Job) studioRecordDTO {
	row := studioRecordDTO{
		ID: job.ID, Kind: string(job.Kind), Status: string(job.Status), Model: job.Model,
		CostUSD: job.CostUSD, AssetID: job.AssetID, Error: job.Error,
		CreatedAt: job.CreatedAt, CompletedAt: job.CompletedAt,
	}
	request, audit, err := job.Submission()
	if err != nil {
		slog.Error("agui: a Studio job's recorded request could not be read; the row is answered without what was asked",
			"job_id", job.ID, "err", sanitizeErr(err))
		return row
	}
	row.Prompt = request.Prompt
	row.Adjustments = audit.Adjustments
	row.Used = studioUsedDTO{
		Duration: request.Duration, Resolution: request.Resolution, AspectRatio: request.AspectRatio,
		Audio: request.GenerateAudio, Seed: request.Seed,
		FirstFrameAssetID: audit.FirstFrameAssetID, LastFrameAssetID: audit.LastFrameAssetID,
		ReferenceAssetIDs: audit.ReferenceAssetIDs,
	}
	return row
}

// studioErrorStatus maps a mediagen refusal code to the status the cockpit acts on: 422 for a
// request it can fix, 409 for a key it must configure, 402 for credit it must buy, 502 for a
// generation that did not deliver. A code minted later and not listed here is a generation
// that did not deliver either, so it falls to 502 rather than to a fabricated 4xx.
var studioErrorStatus = map[string]int{
	"unsupported":     http.StatusUnprocessableEntity,
	"model_rejected":  http.StatusUnprocessableEntity,
	"asset_not_found": http.StatusUnprocessableEntity,
	"too_large":       http.StatusUnprocessableEntity,
	"content_blocked": http.StatusUnprocessableEntity,
	"no_key":          http.StatusConflict,
	"no_credit":       http.StatusPaymentRequired,
	"job_failed":      http.StatusBadGateway,
	"outcome_unknown": http.StatusBadGateway,
}

type studioErrorDTO struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

// writeStudioError answers a refusal the operator can act on, and nothing else. An error with
// no code is an infrastructure failure whose text can name a host, a DSN or a credential, so
// it is logged and answered with one generic sentence.
func writeStudioError(w http.ResponseWriter, err error) {
	var media *mediagen.Error
	switch {
	case errors.Is(err, ErrMediaCatalogLocalRoute):
		writeJSONStatus(w, http.StatusConflict, studioErrorDTO{Code: "local_route", Error: err.Error()})
	case errors.Is(err, assets.ErrWrongModality):
		writeJSONStatus(w, http.StatusUnprocessableEntity, studioErrorDTO{
			Code: "unsupported", Error: "That file is not an image the Studio can use.",
		})
	case errors.Is(err, pgx.ErrNoRows):
		writeJSONStatus(w, http.StatusNotFound, studioErrorDTO{Code: "not_found", Error: "That is not yours, or it is gone."})
	case errors.As(err, &media):
		status, listed := studioErrorStatus[media.Code]
		if !listed {
			status = http.StatusBadGateway
		}
		writeJSONStatus(w, status, studioErrorDTO{Code: media.Code, Error: media.Message})
	default:
		slog.Error("agui: a Studio request failed", "err", sanitizeErr(err))
		writeJSONStatus(w, http.StatusInternalServerError, studioErrorDTO{
			Code: "internal", Error: "The Studio could not complete that request.",
		})
	}
}
