package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	"github.com/navidrome/navidrome/utils/req"
)

// OpenSubsonic apiKeyAuthentication (Sonus): an app password sent as apiKey=, with no username, signs a request. The
// errors are the extension's: 43 when other credentials come along, 44 for a key that is no one's.
const (
	apiKeyExtension      = "apiKeyAuthentication"
	errorConflictingAuth = int32(43)
	errorInvalidAPIKey   = int32(44)
)

func isAPIKeyRequest(r *http.Request) bool {
	key, _ := req.Params(r).String("apiKey")
	return key != ""
}

// authenticateAPIKey is the user an API key belongs to. Failed keys count against the client's address as failed
// passwords do.
func authenticateAPIKey(ctx context.Context, ds model.DataStore, r *http.Request, limiter *authLimiter) (*model.User, error) {
	p := req.Params(r)
	key, _ := p.String("apiKey")
	for _, other := range []string{"u", "p", "t", "s", "jwt"} {
		if v, _ := p.String(other); v != "" {
			return nil, newError(errorConflictingAuth, "Multiple conflicting authentication mechanisms provided")
		}
	}
	slot, allowed := limiter.acquire(ctx, server.ClientIP(r)+"\x00apiKey")
	if !allowed {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Warn(ctx, "API: Too many failed API keys", "remoteAddr", r.RemoteAddr)
		return nil, newError(errorInvalidAPIKey, "Invalid API key")
	}
	client, _ := p.String("c")
	userID, err := twofactor.Instance().UserForAPIKey(ctx, key, client)
	if err != nil {
		slot.release(false)
		log.Error(ctx, "API: Error checking an API key", err)
		return nil, newError(responses.ErrorGeneric, "Internal error")
	}
	if userID == "" {
		slot.release(true)
		log.Warn(ctx, "API: Invalid API key", "remoteAddr", r.RemoteAddr)
		return nil, newError(errorInvalidAPIKey, "Invalid API key")
	}
	user, err := ds.User(ctx).Get(userID)
	slot.release(err != nil)
	if err != nil {
		return nil, newError(errorInvalidAPIKey, "Invalid API key")
	}
	return user, nil
}

// GetTokenInfo is OpenSubsonic's tokenInfo: whose API key signed the request.
func (api *Router) GetTokenInfo(r *http.Request) (*responses.Subsonic, error) {
	user, _ := request.UserFrom(r.Context())
	response := newResponse()
	response.TokenInfo = &responses.TokenInfo{Username: user.UserName}
	return response, nil
}

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
