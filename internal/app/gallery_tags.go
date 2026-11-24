package app

import (
	"encoding/json"

	"pandaopds/internal/eh"
)

// galleryTagsFromValue accepts either the flat list shape used by gallery
// listings or the namespace-grouped shape used by detailed metadata.
func galleryTagsFromValue(value any) []eh.GalleryTag {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var flat []eh.GalleryTag
	if err := json.Unmarshal(raw, &flat); err == nil && flat != nil {
		return flat
	}
	var grouped map[string][]eh.GalleryTag
	if err := json.Unmarshal(raw, &grouped); err != nil {
		return nil
	}
	flat = make([]eh.GalleryTag, 0)
	for namespace, tags := range grouped {
		for _, tag := range tags {
			if tag.Namespace == "" {
				tag.Namespace = namespace
			}
			flat = append(flat, tag)
		}
	}
	return flat
}

func subscriptionGalleryListItem(item map[string]any) (eh.GalleryListItem, bool) {
	if tags, ok := item["tags"]; ok {
		item["tags"] = galleryTagsFromValue(tags)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return eh.GalleryListItem{}, false
	}
	var publication eh.GalleryListItem
	if err := json.Unmarshal(raw, &publication); err != nil {
		return eh.GalleryListItem{}, false
	}
	return publication, true
}
