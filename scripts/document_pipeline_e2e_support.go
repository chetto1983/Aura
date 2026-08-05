//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/chetto1983/aura/internal/webauth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	probeTimeout   = 45 * time.Second
	cleanupTimeout = 5 * time.Minute
)

type e2eIdentity struct {
	Label        string `json:"label"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	AuthulaUser  string `json:"authula_user_id,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	BucketID     string `json:"bucket_id,omitempty"`
	AccessKey    string `json:"access_key,omitempty"`
	ArcadeDBName string `json:"arcade_database,omitempty"`
}

type objectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

type e2eSnapshot struct {
	IdentityID       string            `json:"identity_id"`
	AssetID          string            `json:"asset_id"`
	DocumentID       string            `json:"document_id"`
	SearchDocumentID string            `json:"search_document_id"`
	VersionID        string            `json:"version_id"`
	VersionNumber    int               `json:"version_number"`
	AssetSHA256      string            `json:"asset_sha256"`
	OriginalSHA256   string            `json:"original_sha256"`
	PipelineGen      int64             `json:"pipeline_generation"`
	AssetStatus      string            `json:"asset_status"`
	DocumentStatus   string            `json:"document_status"`
	VersionStatus    string            `json:"version_status"`
	JobStatus        string            `json:"job_status"`
	JobStage         string            `json:"job_stage"`
	JobAttempts      int               `json:"job_attempts"`
	JobLeaseGen      int64             `json:"job_lease_generation"`
	ChunkCount       int               `json:"chunk_count"`
	EmbeddingCount   int               `json:"embedding_count"`
	ProjectionCount  int               `json:"projection_count"`
	CitationTokens   []string          `json:"citation_tokens"`
	Stages           map[string]string `json:"stages"`
	StageProducers   map[string]string `json:"stage_producers"`
	Events           []string          `json:"events"`
	Objects          []objectRef       `json:"objects"`
	EmbedModels      int               `json:"-"`
	EmbedModel       string            `json:"embedding_model"`
	EmbedVersions    int               `json:"-"`
	EmbedVersion     string            `json:"embedding_version"`
	EmbedDimensions  int               `json:"-"`
	EmbedDimension   int               `json:"embedding_dimension"`
}

type e2eState struct {
	RunID      string                 `json:"run_id"`
	Identities []e2eIdentity          `json:"identities"`
	Snapshots  map[string]e2eSnapshot `json:"snapshots,omitempty"`
}

type e2eCredentials struct {
	Cookies map[string]string `json:"cookies"`
}

type runtimeDeps struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	objects  *objectstore.IdentityStore
	garage   *garageadmin.Client
	arcade   *arcadedb.Client
	tenantPW *arcadedb.TenantCredentials
}

func openRuntime(ctx context.Context) (*runtimeDeps, error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, err
	}
	objects, err := objectstore.NewIdentityStore(pool, cfg.AuthulaSecret, objectstore.Credentials{
		Bucket: cfg.ObjectStoreBucket, AccessKey: cfg.ObjectStoreAccessKey, SecretKey: cfg.ObjectStoreSecretKey,
	}, cfg.AuthulaOperatorIdentity)
	if err != nil {
		pool.Close()
		return nil, err
	}
	garage, err := garageadmin.New(cfg.GarageAdminEndpoint, cfg.GarageAdminToken)
	if err != nil {
		pool.Close()
		return nil, err
	}
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL, Database: cfg.ArcadeDB.Database,
		User: cfg.ArcadeDB.AdminUser, Password: cfg.ArcadeDB.AdminPassword})
	if err != nil {
		pool.Close()
		return nil, err
	}
	tenantPW, err := arcadedb.NewTenantCredentials()
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &runtimeDeps{cfg: cfg, pool: pool, objects: objects, garage: garage, arcade: admin, tenantPW: tenantPW}, nil
}

func (r *runtimeDeps) close() {
	if r != nil && r.pool != nil {
		r.pool.Close()
	}
}

func authulaDSN(cfg *config.Config) string {
	if strings.TrimSpace(cfg.AuthulaDatabaseURL) != "" {
		return cfg.AuthulaDatabaseURL
	}
	return cfg.DB.URL
}

func readJSON(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func writePrivateJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func outputJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

func preflight(ctx context.Context, expectedModel, expectedEmbedModel, expectedEmbedVersion, expectedDocling string) (map[string]any, error) {
	if expectedModel == "" || expectedEmbedModel == "" || expectedEmbedVersion == "" || expectedDocling == "" {
		return nil, errors.New("all expected production model identifiers are required")
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return nil, err
	}
	defer r.close()
	if r.cfg.Profile != config.ProfileServerProduction {
		return nil, fmt.Errorf("profile is not server_production")
	}
	if r.cfg.ObjectStoreBackend != "garage" {
		return nil, fmt.Errorf("object store backend is not garage")
	}
	if err := r.cfg.Document.Validate(); err != nil {
		return nil, err
	}
	if err := r.cfg.DocumentPipeline.Validate(); err != nil {
		return nil, err
	}
	if r.cfg.Document.DoclingImage != config.DoclingImageV1290 || strings.TrimSpace(r.cfg.Document.DoclingAPIKey) == "" {
		return nil, fmt.Errorf("Docling production boundary is not pinned and authenticated")
	}
	if r.cfg.Embed.Dimensions <= 0 || strings.TrimSpace(r.cfg.Embed.BaseURL) == "" {
		return nil, fmt.Errorf("embedding production boundary is incomplete")
	}
	actualEmbedModel := strings.TrimSpace(r.cfg.Embed.Model)
	if actualEmbedModel == "" {
		actualEmbedModel = embeddings.DefaultModel
	}
	if expectedEmbedModel != actualEmbedModel || expectedEmbedVersion != r.cfg.Embed.Revision ||
		expectedDocling != r.cfg.Document.DoclingImage {
		return nil, fmt.Errorf("expected document model identifiers do not match production configuration")
	}
	for name, value := range map[string]string{
		"Garage admin token": r.cfg.GarageAdminToken, "Garage endpoint": r.cfg.GarageAdminEndpoint,
		"ArcadeDB admin user": r.cfg.ArcadeDB.AdminUser, "ArcadeDB admin password": r.cfg.ArcadeDB.AdminPassword,
		"Authula secret": r.cfg.AuthulaSecret,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is empty", name)
		}
	}
	if err := db.CheckMigrationHead(ctx, r.cfg.DB.MigrateURL); err != nil {
		return nil, err
	}
	version, err := db.MigrationHead()
	if err != nil || version < 93 {
		return nil, fmt.Errorf("migration floor 93 is not the clean embedded head")
	}
	return map[string]any{
		"profile": r.cfg.Profile, "migration_version": version, "migration_dirty": false,
		"docling_image": r.cfg.Document.DoclingImage, "embedding_dimensions": r.cfg.Embed.Dimensions,
		"expected_agent_model": expectedModel, "expected_embedding_model": expectedEmbedModel,
		"expected_embedding_version": expectedEmbedVersion, "expected_docling_producer": expectedDocling,
	}, nil
}

func setup(ctx context.Context, baseURL, runID, statePath, credentialPath string) error {
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	provider, err := webauth.New(webauth.Config{DSN: authulaDSN(r.cfg), Secret: r.cfg.AuthulaSecret,
		TrustedOrigins: []string{baseURL}, RateLimitMax: r.cfg.AuthulaRateLimitMax})
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()
	core := provider.CoreServices()
	if core == nil || core.UserService == nil || core.SessionService == nil || core.TokenService == nil {
		return errors.New("Authula core services are incomplete")
	}
	state := e2eState{RunID: runID, Snapshots: map[string]e2eSnapshot{}}
	creds := e2eCredentials{Cookies: map[string]string{}}
	if err := writePrivateJSON(statePath, &state); err != nil {
		return err
	}
	if err := writePrivateJSON(credentialPath, &creds); err != nil {
		return err
	}
	for _, label := range []string{"a", "b"} {
		identityID := uuid.NewString()
		for strings.EqualFold(identityID, strings.TrimSpace(r.cfg.AuthulaOperatorIdentity)) {
			identityID = uuid.NewString()
		}
		name := fmt.Sprintf("document-e2e+%s-%s@example.test", runID, label)
		entry := e2eIdentity{Label: label, ID: identityID, Name: name}
		database, err := arcadedb.DatabaseFor(identityID)
		if err != nil {
			return err
		}
		entry.ArcadeDBName = database
		if _, err := r.pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`, identityID, name); err != nil {
			return err
		}
		state.Identities = append(state.Identities, entry)
		if err := writePrivateJSON(statePath, &state); err != nil {
			return err
		}
		if err := db.WithIdentityTxRaw(ctx, r.pool, identityID, func(tx pgx.Tx) error {
			_, execErr := tx.Exec(ctx, `INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, 'agent.run')`, identityID)
			return execErr
		}); err != nil {
			return err
		}
		user, err := core.UserService.Create(ctx, name, name, true, nil, nil)
		if err != nil {
			return err
		}
		entry.AuthulaUser = user.ID
		state.Identities[len(state.Identities)-1] = entry
		if err := writePrivateJSON(statePath, &state); err != nil {
			return err
		}
		if err := webauth.NewIdentityLinker(r.pool).LinkUser(ctx, identityID, user.ID); err != nil {
			return err
		}
		rawToken, err := core.TokenService.Generate()
		if err != nil {
			return err
		}
		if _, err := core.SessionService.Create(ctx, user.ID, core.TokenService.Hash(rawToken), nil, nil, 2*time.Hour); err != nil {
			return err
		}
		creds.Cookies[identityID] = webauth.SessionCookieName + "=" + rawToken
		if err := writePrivateJSON(credentialPath, &creds); err != nil {
			return err
		}
		bucket, err := garageadmin.BucketForIdentity(identityID)
		if err != nil {
			return err
		}
		bucketID, err := r.garage.CreateBucket(ctx, bucket)
		if err != nil {
			return err
		}
		entry.Bucket, entry.BucketID = bucket, bucketID
		state.Identities[len(state.Identities)-1] = entry
		if err := writePrivateJSON(statePath, &state); err != nil {
			return err
		}
		accessKey, secretKey, err := r.garage.CreateKey(ctx, "document-e2e-"+identityID)
		if err != nil {
			return err
		}
		entry.AccessKey = accessKey
		state.Identities[len(state.Identities)-1] = entry
		if err := writePrivateJSON(statePath, &state); err != nil {
			return err
		}
		if err := r.garage.AllowBucketKey(ctx, bucketID, accessKey, garageadmin.ReadWrite); err != nil {
			return err
		}
		if err := r.objects.Put(ctx, identityID, bucket, accessKey, secretKey); err != nil {
			return err
		}
	}
	return outputJSON(state)
}

func cleanup(ctx context.Context, statePath string) error {
	var state e2eState
	if err := readJSON(statePath, &state); err != nil {
		return err
	}
	r, err := openRuntime(ctx)
	if err != nil {
		return err
	}
	defer r.close()
	provider, providerErr := webauth.New(webauth.Config{DSN: authulaDSN(r.cfg), Secret: r.cfg.AuthulaSecret,
		TrustedOrigins: []string{"https://document-e2e.invalid"}, RateLimitMax: r.cfg.AuthulaRateLimitMax})
	if provider != nil {
		defer func() { _ = provider.Close() }()
	}
	var failures []string
	for i := len(state.Identities) - 1; i >= 0; i-- {
		entry := state.Identities[i]
		creds, resolveErr := r.objects.Resolve(identityctx.WithIdentityID(ctx, entry.ID))
		if resolveErr == nil {
			store, buildErr := objectstore.NewS3(ctx, objectstore.S3Config{Endpoint: r.cfg.ObjectStoreEndpoint,
				Region: r.cfg.ObjectStoreRegion, AccessKey: creds.AccessKey, SecretKey: creds.SecretKey, PathStyle: r.cfg.ObjectStorePathStyle})
			if buildErr == nil {
				objects, listErr := store.List(ctx, objectstore.ListRequest{Bucket: creds.Bucket})
				if listErr != nil {
					failures = append(failures, entry.Label+":list_objects")
				} else {
					for _, item := range objects {
						if deleteErr := store.Delete(ctx, item.Ref); deleteErr != nil {
							failures = append(failures, entry.Label+":delete_object")
						}
					}
					remaining, verifyErr := store.List(ctx, objectstore.ListRequest{Bucket: creds.Bucket, Limit: 1})
					if verifyErr != nil || len(remaining) != 0 {
						failures = append(failures, entry.Label+":objects_survived")
					}
				}
			} else {
				failures = append(failures, entry.Label+":build_object_client")
			}
		}
		if err := purgeArcade(ctx, r, entry); err != nil {
			failures = append(failures, entry.Label+":arcade")
		}
		if providerErr == nil && entry.AuthulaUser != "" {
			core := provider.CoreServices()
			if err := core.SessionService.DeleteAllByUserID(ctx, entry.AuthulaUser); err != nil {
				failures = append(failures, entry.Label+":sessions")
			}
			if err := core.UserService.Delete(ctx, entry.AuthulaUser); err != nil {
				failures = append(failures, entry.Label+":authula_user")
			}
		} else if entry.AuthulaUser != "" {
			failures = append(failures, entry.Label+":authula_provider")
		}
		if err := r.objects.Delete(ctx, entry.ID); err != nil {
			failures = append(failures, entry.Label+":object_binding")
		}
		if entry.BucketID != "" {
			if err := r.garage.DeleteBucket(ctx, entry.BucketID); err != nil {
				failures = append(failures, entry.Label+":garage_bucket")
			}
		}
		if entry.AccessKey != "" {
			if err := r.garage.DeleteKey(ctx, entry.AccessKey); err != nil {
				failures = append(failures, entry.Label+":garage_key")
			}
		}
		if _, err := r.pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1::uuid`, entry.ID); err != nil {
			failures = append(failures, entry.Label+":identity")
		} else {
			var remaining int
			if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identities WHERE id = $1::uuid`, entry.ID).Scan(&remaining); err != nil || remaining != 0 {
				failures = append(failures, entry.Label+":identity_survived")
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("cleanup postconditions failed: %s", strings.Join(failures, ","))
	}
	return outputJSON(map[string]any{"run_id": state.RunID, "cleanup": "complete", "identities": len(state.Identities)})
}

func purgeArcade(ctx context.Context, r *runtimeDeps, entry e2eIdentity) error {
	if entry.ArcadeDBName == "" {
		return nil
	}
	_, _ = r.arcade.DropDatabase(ctx, entry.ArcadeDBName)
	exists, err := r.arcade.DatabaseExists(ctx, entry.ArcadeDBName)
	if err != nil || exists {
		return errors.New("ArcadeDB database survived cleanup")
	}
	user := arcadedb.TenantUserFor(entry.ArcadeDBName)
	_ = r.arcade.DropUser(ctx, user)
	probe, err := arcadedb.New(arcadedb.Config{BaseURL: r.cfg.ArcadeDB.BaseURL, Database: entry.ArcadeDBName,
		User: user, Password: r.tenantPW.PasswordFor(entry.ArcadeDBName)})
	if err != nil {
		return err
	}
	accepted, err := probe.CredentialAccepted(ctx)
	if err != nil || accepted {
		return errors.New("ArcadeDB tenant credential survived cleanup")
	}
	return nil
}
