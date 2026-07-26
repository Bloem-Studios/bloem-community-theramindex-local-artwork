package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestManifestRegistersLocalMetadataImageScheme(t *testing.T) {
	raw, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatalf("ReadFile(manifest.json) error = %v", err)
	}

	var manifest struct {
		Capabilities []struct {
			Type     string `json:"type"`
			Metadata struct {
				Schemes []string `json:"schemes"`
			} `json:"metadata"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("Unmarshal(manifest.json) error = %v", err)
	}

	for _, capability := range manifest.Capabilities {
		if capability.Type != "image_resolver.v1" {
			continue
		}
		for _, scheme := range capability.Metadata.Schemes {
			if scheme == "local-metadata" {
				return
			}
		}
		t.Fatalf("image_resolver.v1 schemes = %v, want local-metadata", capability.Metadata.Schemes)
	}

	t.Fatal("manifest has no image_resolver.v1 capability")
}
