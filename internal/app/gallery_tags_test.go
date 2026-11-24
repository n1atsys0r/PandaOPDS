package app

import "testing"

func TestGalleryTagsFromValueSupportsFlatAndGroupedShapes(t *testing.T) {
	flat := []any{map[string]any{"namespace": "artist", "key": "alice", "status": "confidence"}}
	gotFlat := galleryTagsFromValue(flat)
	if len(gotFlat) != 1 || gotFlat[0].String() != "artist:alice" || gotFlat[0].Status != "confidence" {
		t.Fatalf("flat tags=%#v", gotFlat)
	}
	grouped := map[string]any{"female": []any{map[string]any{"key": "catgirl", "status": "tagged"}}}
	gotGrouped := galleryTagsFromValue(grouped)
	if len(gotGrouped) != 1 || gotGrouped[0].String() != "female:catgirl" || gotGrouped[0].Status != "tagged" {
		t.Fatalf("grouped tags=%#v", gotGrouped)
	}
}
