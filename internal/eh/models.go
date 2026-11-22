package eh

type GalleryTag struct {
	Namespace string            `json:"namespace"`
	Key       string            `json:"key"`
	Status    string            `json:"status,omitempty"`
	Style     map[string]string `json:"style,omitempty"`
}

func (t GalleryTag) String() string { return t.Namespace + ":" + t.Key }

type GalleryListItem struct {
	GID   int    `json:"gid"`
	Token string `json:"token"`
	// ActiveGID and ActiveToken identify the current archive version while GID
	// and Token remain the stable public identity of the original gallery.
	ActiveGID   int          `json:"-"`
	ActiveToken string       `json:"-"`
	Title       string       `json:"title"`
	Category    string       `json:"category"`
	CoverURL    string       `json:"cover_url"`
	PageCount   *int         `json:"page_count,omitempty"`
	Rating      float64      `json:"rating"`
	PublishTime string       `json:"publish_time,omitempty"`
	Language    string       `json:"language,omitempty"`
	Expunged    bool         `json:"expunged,omitempty"`
	Favcat      *int         `json:"favcat,omitempty"`
	Tags        []GalleryTag `json:"tags,omitempty"`
	Uploader    string       `json:"uploader,omitempty"`
}

type GalleryPageInfo struct {
	Galleries  []GalleryListItem `json:"galleries"`
	NextGID    string            `json:"next_gid,omitempty"`
	PrevGID    string            `json:"prev_gid,omitempty"`
	TotalCount *int              `json:"total_count,omitempty"`
	NextPage   *int              `json:"next_page,omitempty"`
	FavcatMap  map[int]string    `json:"favcat_map,omitempty"`
}

type Thumbnail struct {
	Href        string   `json:"href"`
	ThumbURL    string   `json:"thumb_url"`
	PageNo      int      `json:"page_no"`
	Width       float64  `json:"width,omitempty"`
	Height      float64  `json:"height,omitempty"`
	OriginHash  string   `json:"origin_image_hash,omitempty"`
	BackgroundX *float64 `json:"background_x,omitempty"`
	BackgroundY *float64 `json:"background_y,omitempty"`
	IsSprite    *bool    `json:"is_sprite,omitempty"`
	IsLarge     bool     `json:"is_large,omitempty"`
}

type GalleryComment struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	UserID       *int   `json:"user_id,omitempty"`
	Time         string `json:"time,omitempty"`
	LastEditTime string `json:"last_edit_time,omitempty"`
	ContentHTML  string `json:"content_html,omitempty"`
}

type GalleryVersion struct {
	GID     int    `json:"gid"`
	Token   string `json:"token"`
	Title   string `json:"title"`
	AddedAt string `json:"added_at,omitempty"`
}

type DetailPageInfo struct {
	ActiveGID     int              `json:"-"`
	ActiveToken   string           `json:"-"`
	Favcat        *int             `json:"-"`
	FavcatName    string           `json:"-"`
	ImageNoFrom   int              `json:"image_no_from"`
	ImageNoTo     int              `json:"image_no_to"`
	ImageCount    int              `json:"image_count"`
	CurrentPageNo int              `json:"current_page_no"`
	PageCount     int              `json:"page_count"`
	Thumbnails    []Thumbnail      `json:"thumbnails"`
	Tags          []GalleryTag     `json:"tags,omitempty"`
	Title         string           `json:"title"`
	TitleJPN      string           `json:"title_jpn,omitempty"`
	Category      string           `json:"category,omitempty"`
	CoverURL      string           `json:"cover_url,omitempty"`
	Rating        float64          `json:"rating,omitempty"`
	Uploader      string           `json:"uploader,omitempty"`
	PublishTime   string           `json:"publish_time,omitempty"`
	Language      string           `json:"language,omitempty"`
	FilesizeText  string           `json:"filesize_text,omitempty"`
	FilesizeBytes *int64           `json:"filesize_bytes,omitempty"`
	TorrentCount  int              `json:"torrent_count,omitempty"`
	Expunged      bool             `json:"expunged,omitempty"`
	Comments      []GalleryComment `json:"comments,omitempty"`
	NewerVersions []GalleryVersion `json:"newer_versions,omitempty"`
}

type ImagePageInfo struct {
	ImageURL  string   `json:"image_url"`
	Width     *float64 `json:"width,omitempty"`
	Height    *float64 `json:"height,omitempty"`
	ReloadKey string   `json:"reload_key,omitempty"`
	Is509     bool     `json:"is_509,omitempty"`
}

type GalleryMetadata struct {
	GID          int                     `json:"gid"`
	Token        string                  `json:"token"`
	Title        string                  `json:"title"`
	TitleJPN     string                  `json:"title_jpn"`
	Category     string                  `json:"category"`
	Thumb        string                  `json:"thumb"`
	Rating       float64                 `json:"rating"`
	Tags         map[string][]GalleryTag `json:"tags"`
	Filecount    int                     `json:"filecount"`
	Filesize     int64                   `json:"filesize"`
	Posted       int64                   `json:"posted"`
	Uploader     string                  `json:"uploader"`
	Torrentcount int                     `json:"torrentcount"`
	Expunged     bool                    `json:"expunged"`
	ParentGID    int                     `json:"parent_gid,omitempty"`
	ParentKey    string                  `json:"parent_key,omitempty"`
	FirstGID     int                     `json:"first_gid,omitempty"`
	FirstKey     string                  `json:"first_key,omitempty"`
	CurrentGID   int                     `json:"current_gid,omitempty"`
	CurrentKey   string                  `json:"current_key,omitempty"`
}

type GalleryRef struct {
	GID   int    `json:"gid"`
	Token string `json:"token"`
}

func (m GalleryMetadata) Language() string {
	for _, t := range m.Tags["language"] {
		if v := language(t.Key); v != "" {
			return v
		}
	}
	return ""
}

var languages = map[string]string{"english": "en", "japanese": "ja", "chinese": "zh", "korean": "ko", "french": "fr", "german": "de", "spanish": "es", "russian": "ru", "italian": "it", "portuguese": "pt", "thai": "th", "vietnamese": "vi", "indonesian": "id", "arabic": "ar", "polish": "pl", "dutch": "nl", "swedish": "sv", "norwegian": "no", "finnish": "fi", "danish": "da", "turkish": "tr", "hungarian": "hu", "czech": "cs", "greek": "el", "hebrew": "he", "persian": "fa", "ukrainian": "uk", "brazilian": "pt-BR", "latin american": "es-419"}

func language(key string) string { return languages[key] }
