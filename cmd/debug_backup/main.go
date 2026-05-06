package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/backup"
	auraconfig "github.com/aura/aura/internal/config"
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
	if missing := backup.MissingConfig(bcfg); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: missing Garage backup config: %s\n", strings.Join(missing, ", "))
		fmt.Fprintf(os.Stderr, "config: %s\n", bcfg.Redacted())
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	uploader, err := backup.NewS3Uploader(ctx, bcfg)
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
