package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore"
)

// The file manager's delete retires the asset row of each key it removes, so the browser it is
// built on must carry the asset store whenever there is a database to hold one.
func TestBuildFileBrowserCarriesTheAssetRows(t *testing.T) {
	cfg := &config.Config{ObjectStoreBucket: "aura-assets"}
	objects := objectstore.NewFake()
	if browser := buildFileBrowser(cfg, nil, objects); browser.Rows != nil {
		t.Fatalf("a pool-less browser has rows %T", browser.Rows)
	}
	if browser := buildFileBrowser(cfg, newLazyPool(t), objects); browser.Rows == nil {
		t.Fatal("the file manager would delete objects and leave their asset rows live")
	}
}
