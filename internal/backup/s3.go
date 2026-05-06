package backup

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	Category     string
	Timestamp    string
}

type Manager struct {
	cfg    Config
	client *s3.Client
}

func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	if missing := MissingConfig(cfg); len(missing) > 0 {
		return nil, fmt.Errorf("missing Garage backup config: %s", strings.Join(missing, ", "))
	}
	client, err := newS3Client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, client: client}, nil
}

func MissingConfig(cfg Config) []string {
	var missing []string
	if strings.TrimSpace(cfg.Endpoint) == "" {
		missing = append(missing, "GARAGE_S3_ENDPOINT")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		missing = append(missing, "GARAGE_S3_REGION")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		missing = append(missing, "GARAGE_S3_BUCKET")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" {
		missing = append(missing, "GARAGE_S3_ACCESS_KEY")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		missing = append(missing, "GARAGE_S3_SECRET_KEY")
	}
	return missing
}

func (m *Manager) ExportArtifactSet(ctx context.Context, now time.Time) (ArtifactSetResult, error) {
	return ExportArtifactSet(ctx, m.cfg, m, now)
}

func (m *Manager) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := m.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	return err
}

func (m *Manager) ListObjects(ctx context.Context) ([]Object, error) {
	var out []Object
	p := s3.NewListObjectsV2Paginator(m.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(m.cfg.Bucket),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			out = append(out, Object{
				Key:          key,
				Size:         aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
				Category:     CategoryForKey(key),
				Timestamp:    TimestampForKey(key),
			})
		}
	}
	return out, nil
}

func CategoryForKey(key string) string {
	switch {
	case strings.HasPrefix(key, "backups/"):
		return "full_restore"
	case strings.HasSuffix(key, "/source-originals.tar.gz"):
		return "source_originals"
	case strings.HasSuffix(key, "/extractions.tar.gz"):
		return "extractions"
	case strings.HasSuffix(key, "/memory-snapshot.tar.gz"):
		return "memory_snapshot"
	case strings.HasSuffix(key, "/embedding-index.tar.gz"):
		return "embedding_index"
	case strings.HasSuffix(key, "/audit-bundle.tar.gz"):
		return "audit_bundle"
	case strings.HasSuffix(key, "/manifest.json"):
		return "manifest"
	default:
		return "artifact"
	}
}

func TimestampForKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) >= 2 && (parts[0] == "backups" || parts[0] == "artifacts") {
		return parts[1]
	}
	return ""
}

func NewS3Uploader(ctx context.Context, cfg Config) (*Manager, error) {
	return NewManager(ctx, cfg)
}

func newS3Client(ctx context.Context, cfg Config) (*s3.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	}), nil
}
