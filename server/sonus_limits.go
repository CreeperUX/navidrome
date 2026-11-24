package server

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// MaxAPIBodySize bounds request bodies on the native API and the Jellyfin API, whose handlers decode JSON with no limit
// of their own (playlists, queue, libraries, plugin settings, quick connect). 4 MiB is about 150,000 song ids, far
// above any playlist or queue anyone sends. Uploads keep their own limits (MaxImageUploadSize): the native API's are
// multipart/form-data, the Jellyfin API's an image/* body (base64).
// Upstream bounded these after 0.64.2, per route (#6505, #6506); this fork stopped following upstream there, so it
// bounds them here, at the router.
const MaxAPIBodySize = 4 << 20

// LimitRequestBody answers 413 to a body declared larger than limit, and stops reading any body past it (the handler's
// JSON decoding then fails), except for uploads.
func LimitRequestBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil || r.Body == http.NoBody || isUpload(r) {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > limit {
				http.Error(w, fmt.Sprintf("Request body must not be larger than %d bytes", limit), http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// isUpload: a request whose handler bounds the body itself (and, for multipart, a body that is never decoded as JSON)
func isUpload(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && (mediaType == "multipart/form-data" || strings.HasPrefix(mediaType, "image/"))
}
