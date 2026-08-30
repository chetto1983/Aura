//nolint:revive // Internal objectstore S3 types are exported for composition roots.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

type S3Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	AccessKey      string
	SecretKey      string
	PathStyle      bool
}

type S3Store struct {
	client          *s3.Client
	presign         *s3.PresignClient
	publicEndpoint  string
	presignEndpoint string
	awsCfg          aws.Config
	pathStyle       bool
}

func NewS3(ctx context.Context, cfg S3Config) (*S3Store, error) {
	region := cfg.Region
	if region == "" {
		region = "garage"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := newS3Client(awsCfg, cfg.Endpoint, cfg.PathStyle)
	presignEndpoint := cfg.Endpoint
	if cfg.PublicEndpoint != "" {
		presignEndpoint = cfg.PublicEndpoint
	}
	presignClient := client
	if presignEndpoint != cfg.Endpoint {
		presignClient = newS3Client(awsCfg, presignEndpoint, cfg.PathStyle)
	}
	return &S3Store{
		client:          client,
		presign:         s3.NewPresignClient(presignClient),
		publicEndpoint:  cfg.PublicEndpoint,
		presignEndpoint: presignEndpoint,
		awsCfg:          awsCfg,
		pathStyle:       cfg.PathStyle,
	}, nil
}

func (s *S3Store) PresignPut(ctx context.Context, req PresignPutRequest) (PresignedPut, error) {
	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	presigner := s.presign
	if req.PublicBase != "" && req.PublicBase != s.presignEndpoint {
		presigner = s3.NewPresignClient(newS3Client(s.awsCfg, req.PublicBase, s.pathStyle))
	}
	out, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(req.Ref.Bucket),
		Key:         aws.String(req.Ref.Key),
		ContentType: aws.String(req.MIMEType),
		Metadata:    req.Metadata,
	}, func(o *s3.PresignOptions) {
		o.Expires = expiresIn
	})
	if err != nil {
		return PresignedPut{}, err
	}
	// The metadata headers are SIGNED above, so they are not optional: a client that drops
	// one gets a signature mismatch, not an object without a name. Returning them here is
	// what lets the client stay ignorant of the contract — it forwards RequiredHeaders
	// verbatim.
	return PresignedPut{
		URL:             out.URL,
		Method:          "PUT",
		RequiredHeaders: presignRequiredHeaders(req.MIMEType, req.Metadata),
		ExpiresAt:       time.Now().Add(expiresIn),
	}, nil
}

func newS3Client(awsCfg aws.Config, endpoint string, pathStyle bool) *s3.Client {
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = pathStyle
		// AWS SDK Go v2 (s3 >=1.66) defaults checksum calculation to "when_supported",
		// which streams PutObject with an aws-chunked CRC32 trailer + STREAMING content
		// hash that Garage rejects (400 InvalidDigest: x-amz-content-sha256 mismatch).
		// Restrict to "when_required" so uploads use a plain signed payload Garage accepts.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.EndpointResolverV2 = s3EndpointResolver{
			endpoint: endpoint,
			next:     s3.NewDefaultEndpointResolverV2(),
		}
	})
}

func (s *S3Store) ConfigureBrowserUploadCORS(ctx context.Context, bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return fmt.Errorf("objectstore s3 cors bucket is empty")
	}
	_, err := s.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: browserUploadCORSConfiguration(),
	})
	return err
}

func (s *S3Store) Put(ctx context.Context, ref ObjectRef, body io.Reader, opts PutOptions) (Attrs, error) {
	body, cleanup, err := seekableBody(body)
	if err != nil {
		return Attrs{}, err
	}
	defer cleanup()
	in := &s3.PutObjectInput{
		Bucket:      aws.String(ref.Bucket),
		Key:         aws.String(ref.Key),
		Body:        body,
		ContentType: aws.String(opts.MIMEType),
		Metadata:    opts.Metadata,
	}
	// Only declare ContentLength when the size is actually known. A zero here for a
	// non-empty body makes the SDK sign x-amz-content-sha256 over the real bytes while
	// sending Content-Length: 0 → Garage rejects with 400 InvalidDigest. Omitting it lets
	// the SDK derive the length + payload hash from a seekable body.
	if opts.Size > 0 {
		in.ContentLength = aws.Int64(opts.Size)
	}
	out, err := s.client.PutObject(ctx, in)
	if err != nil {
		return Attrs{}, err
	}
	return Attrs{
		SizeBytes: opts.Size,
		ETag:      strings.Trim(aws.ToString(out.ETag), `"`),
		MIMEType:  opts.MIMEType,
	}, nil
}

func (s *S3Store) Head(ctx context.Context, ref ObjectRef) (Attrs, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(ref.Bucket),
		Key:    aws.String(ref.Key),
	})
	if err != nil {
		return Attrs{}, err
	}
	return Attrs{
		SizeBytes: aws.ToInt64(out.ContentLength),
		ETag:      strings.Trim(aws.ToString(out.ETag), `"`),
		MIMEType:  aws.ToString(out.ContentType),
		Metadata:  out.Metadata,
	}, nil
}

func (s *S3Store) Get(ctx context.Context, ref ObjectRef) (io.ReadCloser, Attrs, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(ref.Bucket),
		Key:    aws.String(ref.Key),
	})
	if err != nil {
		return nil, Attrs{}, err
	}
	return out.Body, Attrs{
		SizeBytes: aws.ToInt64(out.ContentLength),
		ETag:      strings.Trim(aws.ToString(out.ETag), `"`),
		MIMEType:  aws.ToString(out.ContentType),
		// A GET answers with the user metadata a HEAD would, and dropping it here is what made
		// the file manager offer "deedee78-….md" as the download name for a file the same
		// listing had just called saggio_crittografia.md: the name was in the response all
		// along. Reading it from the object rather than from the index also names an object
		// the sidecar has not reached yet.
		Metadata: out.Metadata,
	}, nil
}

func (s *S3Store) List(ctx context.Context, req ListRequest) ([]ObjectInfo, error) {
	if strings.TrimSpace(req.Bucket) == "" {
		return nil, fmt.Errorf("objectstore s3 bucket is empty")
	}
	if req.Limit < 0 {
		return nil, fmt.Errorf("objectstore s3 limit must not be negative")
	}
	var out []ObjectInfo
	var token *string
	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(req.Bucket),
			Prefix:            aws.String(req.Prefix),
			ContinuationToken: token,
		}
		if req.Delimiter != "" {
			input.Delimiter = aws.String(req.Delimiter)
		}
		if req.Limit > 0 {
			remaining := min(req.Limit-len(out), math.MaxInt32)
			input.MaxKeys = aws.Int32(int32(remaining)) //nolint:gosec // clamped to int32 above.
		}
		resp, err := s.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}
		// Groups first, so one page reads as "folders, then files" without the caller
		// sorting. CommonPrefixes are only populated when a delimiter was sent.
		for _, prefix := range resp.CommonPrefixes {
			out = append(out, ObjectInfo{
				Ref: ObjectRef{Bucket: req.Bucket, Key: aws.ToString(prefix.Prefix)},
			})
		}
		for _, obj := range resp.Contents {
			out = append(out, ObjectInfo{
				Ref: ObjectRef{Bucket: req.Bucket, Key: aws.ToString(obj.Key)},
				Attrs: Attrs{
					SizeBytes:  aws.ToInt64(obj.Size),
					ETag:       strings.Trim(aws.ToString(obj.ETag), `"`),
					ModifiedAt: aws.ToTime(obj.LastModified),
				},
			})
		}
		if req.Limit > 0 && len(out) >= req.Limit {
			return out[:req.Limit], nil
		}
		if !aws.ToBool(resp.IsTruncated) {
			return out, nil
		}
		token = resp.NextContinuationToken
	}
}

func (s *S3Store) Delete(ctx context.Context, ref ObjectRef) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(ref.Bucket),
		Key:    aws.String(ref.Key),
	})
	return err
}

// Copy asks the STORE to duplicate the object; the bytes never travel to this process.
// CopySource is a path, so both components need escaping -- a key with a space or a '+' in
// it is common in a user's corpus and would otherwise resolve to a different object.
func (s *S3Store) Copy(ctx context.Context, src, dst ObjectRef) error {
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dst.Bucket),
		Key:        aws.String(dst.Key),
		CopySource: aws.String(url.PathEscape(src.Bucket + "/" + src.Key)),
	})
	return err
}

func browserUploadCORSConfiguration() *s3types.CORSConfiguration {
	return &s3types.CORSConfiguration{
		CORSRules: []s3types.CORSRule{
			{
				AllowedHeaders: []string{"*"},
				AllowedMethods: []string{"PUT", "GET", "HEAD"},
				AllowedOrigins: []string{"*"},
				ExposeHeaders:  []string{"ETag"},
				MaxAgeSeconds:  aws.Int32(3600),
			},
		},
	}
}

type s3EndpointResolver struct {
	endpoint string
	next     s3.EndpointResolverV2
}

func (r s3EndpointResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if r.endpoint != "" {
		params.Endpoint = aws.String(r.endpoint)
	}
	return r.next.ResolveEndpoint(ctx, params)
}
