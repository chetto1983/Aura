//nolint:revive // Internal objectstore S3 types are exported for composition roots.
package objectstore

import (
	"context"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	client         *s3.Client
	presign        *s3.PresignClient
	publicEndpoint string
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
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.PathStyle
		o.EndpointResolverV2 = s3EndpointResolver{
			endpoint: cfg.Endpoint,
			next:     s3.NewDefaultEndpointResolverV2(),
		}
	})
	return &S3Store{
		client:         client,
		presign:        s3.NewPresignClient(client),
		publicEndpoint: cfg.PublicEndpoint,
	}, nil
}

func (s *S3Store) PresignPut(ctx context.Context, req PresignPutRequest) (PresignedPut, error) {
	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	out, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(req.Ref.Bucket),
		Key:           aws.String(req.Ref.Key),
		ContentType:   aws.String(req.MIMEType),
		ContentLength: aws.Int64(req.Size),
	}, func(o *s3.PresignOptions) {
		o.Expires = expiresIn
	})
	if err != nil {
		return PresignedPut{}, err
	}
	signedURL := out.URL
	publicBase := req.PublicBase
	if publicBase == "" {
		publicBase = s.publicEndpoint
	}
	if publicBase != "" {
		var rewriteErr error
		signedURL, rewriteErr = rewritePresignedHost(signedURL, publicBase)
		if rewriteErr != nil {
			return PresignedPut{}, rewriteErr
		}
	}
	return PresignedPut{
		URL:    signedURL,
		Method: "PUT",
		RequiredHeaders: map[string]string{
			"Content-Type":   req.MIMEType,
			"Content-Length": strconv.FormatInt(req.Size, 10),
		},
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
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

func (s *S3Store) Delete(ctx context.Context, ref ObjectRef) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(ref.Bucket),
		Key:    aws.String(ref.Key),
	})
	return err
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

func rewritePresignedHost(raw, publicBase string) (string, error) {
	signed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(publicBase)
	if err != nil {
		return "", err
	}
	if base.Scheme != "" {
		signed.Scheme = base.Scheme
	}
	if base.Host != "" {
		signed.Host = base.Host
	}
	return signed.String(), nil
}
