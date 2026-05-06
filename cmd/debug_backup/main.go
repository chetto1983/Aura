package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/backup"
	auraconfig "github.com/aura/aura/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	mode := flag.String("mode", "all", "export mode: full, artifacts, or all")
	timeout := flag.Duration("timeout", 2*time.Minute, "backup upload timeout")
	flag.Parse()

	if err := loadDotEnv(auraconfig.EnvPathFromEnvironment()); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: could not load env file: %v\n", err)
	}
	cfg, err := auraconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: load config: %v\n", err)
		os.Exit(1)
	}

	bcfg := backup.Config{
		Endpoint:   cfg.GarageS3Endpoint,
		Region:     cfg.GarageS3Region,
		Bucket:     cfg.GarageS3Bucket,
		AccessKey:  cfg.GarageS3AccessKey,
		SecretKey:  cfg.GarageS3SecretKey,
		EnvPath:    cfg.EnvPath,
		DBPath:     cfg.DBPath,
		WikiPath:   cfg.WikiPath,
		SkillsPath: cfg.SkillsPath,
		LogDir:     cfg.LogDir,
		AuditPaths: []string{"reports"},
	}
	if missing := missingGarageConfig(bcfg); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: missing Garage backup config: %s\n", strings.Join(missing, ", "))
		fmt.Fprintf(os.Stderr, "config: %s\n", bcfg.Redacted())
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	uploader, err := newS3Uploader(ctx, bcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: create S3 uploader: %v\n", err)
		os.Exit(1)
	}
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "", "all", "artifacts":
		res, err := backup.ExportArtifactSet(ctx, bcfg, uploader, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: export artifact set: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("artifact set uploaded bucket=%s timestamp=%s objects=%d\n", res.Bucket, res.Timestamp, len(res.Objects))
		for _, obj := range res.Objects {
			fmt.Printf("- category=%s key=%s files=%d bytes=%d\n", obj.Category, obj.Key, obj.Files, obj.Bytes)
		}
	case "full":
		res, err := backup.Export(ctx, bcfg, uploader, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: export backup: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("backup uploaded bucket=%s key=%s files=%d\n", res.Bucket, res.Key, res.Files)
	default:
		fmt.Fprintf(os.Stderr, "FAIL: invalid mode %q (want full, artifacts, or all)\n", *mode)
		os.Exit(1)
	}
}

type s3Uploader struct {
	client *s3.Client
}

func newS3Uploader(ctx context.Context, cfg backup.Config) (*s3Uploader, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})
	return &s3Uploader{client: client}, nil
}

func (u *s3Uploader) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	return err
}

func missingGarageConfig(cfg backup.Config) []string {
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

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
