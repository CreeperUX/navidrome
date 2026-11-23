package jellyfin

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// passwordAccepted is the sign-in's password check with two-step verification (Sonus, core/twofactor): a user who has
// it set up signs in with an app password, never the account password (Jellyfin clients cannot ask for a code);
// everyone else as upstream has it, or with an app password too.
func passwordAccepted(r *http.Request, user *model.User, password string) bool {
	ctx := r.Context()
	store := twofactor.Instance()
	enabled, err := store.Enabled(ctx, user.ID)
	if err != nil {
		log.Error(ctx, "Jellyfin API: could not read two-step verification", "username", user.UserName, err)
		return false
	}
	if !enabled && subtle.ConstantTimeCompare([]byte(user.Password), []byte(password)) == 1 {
		return true
	}
	a := parseMediaBrowserAuth(r)
	client := strings.TrimSpace(a.Client + " " + a.Device)
	ok, err := store.MatchAppPassword(ctx, user.ID, func(p string) bool {
		return subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1
	}, client)
	if err != nil {
		log.Error(ctx, "Jellyfin API: could not check app passwords", "username", user.UserName, err)
	}
	return ok
}
