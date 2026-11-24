package subsonic

import (
	"net/http"

	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/connect"
	"github.com/navidrome/navidrome/server/subsonic/responses"
)

func (api *Router) GetOpenSubsonicExtensions(_ *http.Request) (*responses.Subsonic, error) {
	response := newResponse()
	extensions := responses.OpenSubsonicExtensions{
		{Name: "transcodeOffset", Versions: []int32{1}},
		{Name: "formPost", Versions: []int32{1}},
		{Name: "songLyrics", Versions: []int32{1, 2}},
		{Name: "indexBasedQueue", Versions: []int32{1}},
		{Name: "transcoding", Versions: []int32{1}},
		{Name: "playbackReport", Versions: []int32{1}},
		{Name: "topSongsByArtistId", Versions: []int32{1}},
		{Name: connect.Extension, Versions: []int32{connect.Version}}, // Sonus Connect (server/connect)
		{Name: apiKeyExtension, Versions: []int32{1}},                 // Sonus: app passwords as API keys (sonus_auth.go)
		{Name: server.DeviceLoginExtension, Versions: []int32{1}},     // Sonus: the browser sign-in for apps
	}
	if api.sonic != nil && api.sonic.HasProvider() {
		extensions = append(extensions, responses.OpenSubsonicExtension{
			Name: "sonicSimilarity", Versions: []int32{1},
		})
	}
	response.OpenSubsonicExtensions = &extensions
	return response, nil
}
