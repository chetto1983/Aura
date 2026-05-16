package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/aura/aura/internal/backup"
)

const backupAPITimeout = 3 * time.Minute

func handleBackupList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, bucket, ok := backupService(w, r, deps)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), backupAPITimeout)
		defer cancel()
		objects, err := svc.ListObjects(ctx)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadGateway, "list Garage backups: "+err.Error())
			return
		}
		sort.Slice(objects, func(i, j int) bool {
			return objects[i].LastModified.After(objects[j].LastModified)
		})
		out := make([]BackupObject, 0, len(objects))
		for _, obj := range objects {
			out = append(out, BackupObject{
				Key:          obj.Key,
				Category:     obj.Category,
				Timestamp:    obj.Timestamp,
				SizeBytes:    obj.Size,
				LastModified: obj.LastModified,
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, BackupListResponse{Bucket: bucket, Objects: out})
	}
}

func handleBackupExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, _, ok := backupService(w, r, deps)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), backupAPITimeout)
		defer cancel()
		res, err := svc.ExportArtifactSet(ctx, time.Now())
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadGateway, "export Garage backup: "+err.Error())
			return
		}
		objects := make([]BackupExportObject, 0, len(res.Objects))
		for _, obj := range res.Objects {
			objects = append(objects, BackupExportObject{
				Category:  obj.Category,
				Key:       obj.Key,
				SizeBytes: obj.Bytes,
				Files:     obj.Files,
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, BackupExportResponse{
			Bucket:    res.Bucket,
			Timestamp: res.Timestamp,
			Objects:   objects,
		})
	}
}

func backupService(w http.ResponseWriter, r *http.Request, deps Deps) (BackupService, string, bool) {
	if deps.Backups != nil {
		bucket := ""
		if deps.RuntimeConfig != nil {
			bucket = deps.RuntimeConfig.GarageS3Bucket
		}
		return deps.Backups, bucket, true
	}
	if deps.RuntimeConfig == nil {
		writeError(w, deps.Logger, http.StatusServiceUnavailable, "Garage backup config unavailable")
		return nil, "", false
	}
	cfg := backupConfigFromRuntime(deps)
	if missing := backup.MissingConfig(cfg); len(missing) > 0 {
		writeError(w, deps.Logger, http.StatusServiceUnavailable, fmt.Sprintf("Garage backup config incomplete: %v", missing))
		return nil, cfg.Bucket, false
	}
	svc, err := backup.NewManager(r.Context(), cfg)
	if err != nil {
		writeError(w, deps.Logger, http.StatusBadGateway, "connect Garage: "+err.Error())
		return nil, cfg.Bucket, false
	}
	return svc, cfg.Bucket, true
}

func backupConfigFromRuntime(deps Deps) backup.Config {
	cfg := deps.RuntimeConfig
	return backup.Config{
		Endpoint:   cfg.GarageS3Endpoint,
		Region:     cfg.GarageS3Region,
		Bucket:     cfg.GarageS3Bucket,
		AccessKey:  cfg.GarageS3AccessKey,
		SecretKey:  cfg.GarageS3SecretKey,
		DBPath:     cfg.DBPath,
		WikiPath:   cfg.WikiPath,
		SkillsPath: cfg.SkillsPath,
		LogDir:     cfg.LogDir,
		AuditPaths: []string{"reports"},
	}
}
