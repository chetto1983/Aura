//nolint:revive // Internal objectstore S3 types are exported for composition roots.
package objectstore

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
	}, func(o *s3.PresignOptions) {
		o.Expires = expiresIn
	})
	if err != nil {
		return PresignedPut{}, err
	}
	return PresignedPut{
		URL:    out.URL,
		Method: "PUT",
		RequiredHeaders: map[string]string{
			"Content-Type": req.MIMEType,
		},
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

func newS3Client(awsCfg aws.Config, endpoint string, pathStyle bool) *s3.Client {
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = pathStyle
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
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(ref.Bucket),
		Key:           aws.String(ref.Key),
		Body:          body,
		ContentType:   aws.String(opts.MIMEType),
		ContentLength: aws.Int64(opts.Size),
	})
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
	}, nil
}

func (s *S3Store) List(ctx context.Context, req ListRequest) ([]ObjectInfo, error) {
	if strings.TrimSpace(req.Bucket) == "" {
		return nil, fmt.Errorf("objectstore s3 bucket is empty")
	}
	var out []ObjectInfo
	var token *string
	for {
		resp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(req.Bucket),
			Prefix:            aws.String(req.Prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, obj := range resp.Contents {
			out = append(out, ObjectInfo{
				Ref: ObjectRef{Bucket: req.Bucket, Key: aws.ToString(obj.Key)},
				Attrs: Attrs{
					SizeBytes: aws.ToInt64(obj.Size),
					ETag:      strings.Trim(aws.ToString(obj.ETag), `"`),
				},
			})
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
