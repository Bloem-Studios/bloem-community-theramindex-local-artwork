package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestManifestRegistersLocalArtworkImageSchemes(t *testing.T) {
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
		want := map[string]bool{"local-artwork": false, "local-metadata": false}
		for _, scheme := range capability.Metadata.Schemes {
			if _, ok := want[scheme]; ok {
				want[scheme] = true
			}
		}
		for scheme, found := range want {
			if !found {
				t.Fatalf("image_resolver.v1 schemes = %v, missing %s", capability.Metadata.Schemes, scheme)
			}
		}
		return
	}

	t.Fatal("manifest has no image_resolver.v1 capability")
}
