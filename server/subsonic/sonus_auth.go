package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/model"
)

// checkCredentials is validateCredentials with two-step verification (Sonus, core/twofactor). A user who has it set up
// signs the API with an app password (as p= or as t= with s=), never the account password: clients cannot ask for a
// code. Everyone else signs in as upstream has it, and may use an app password too. A session token (jwt=, the web UI)
// is as upstream: it was had with the code.
func checkCredentials(ctx context.Context, user *model.User, pass, token, salt, jwt, client string) error {
	if jwt != "" {
		return validateCredentials(user, "", "", "", jwt)
	}
	store := twofactor.Instance()
	enabled, err := store.Enabled(ctx, user.ID)
	if err != nil {
		return err
	}
	if !enabled && validateCredentials(user, pass, token, salt, "") == nil {
		return nil
	}
	if strings.HasPrefix(pass, "enc:") {
		if dec, err := hex.DecodeString(pass[4:]); err == nil {
			pass = string(dec)
		}
	}
	if pass == "" && token == "" {
		return model.ErrInvalidAuth
	}
	ok, err := store.MatchAppPassword(ctx, user.ID, func(password string) bool {
		if pass != "" {
			return subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
		}
		return fmt.Sprintf("%x", md5.Sum([]byte(password+salt))) == token
	}, client)
	if err != nil {
		return err
	}
	if !ok {
		return model.ErrInvalidAuth
	}
	return nil
}
