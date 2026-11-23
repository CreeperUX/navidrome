package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// Two-step verification at the web sign-in (Sonus, core/twofactor). After the password, a user who has it set up gives
// an authenticator or recovery code too ("twoFactor": "code"); one who has not, while the policy is Required, gets a
// short-lived setup token instead of a session and sets it up first ("twoFactor": "setup"). The session comes once the
// code is right. Signed in, users see and manage their own (status, recovery codes, app passwords); admins can reset
// someone's for a lost phone.

// audienceTwoFactorSetup scopes the setup token: no API takes it, only the setup routes below.
const audienceTwoFactorSetup = "sonus-2fa-setup"

const setupTokenLifetime = 15 * time.Minute

func respondTwoFactor(w http.ResponseWriter, status int, step, message string, extra map[string]any) {
	body := map[string]any{"error": message, "twoFactor": step}
	for k, v := range extra {
		body[k] = v
	}
	_ = rest.RespondWithJSON(w, status, body)
}

// twoFactorSignIn runs after a correct password: true when it answered the request itself (a code is missing or wrong,
// or the user must set it up first), false when the sign-in goes on.
func twoFactorSignIn(ctx context.Context, w http.ResponseWriter, user *model.User, code string) bool {
	mode := twofactor.CurrentMode()
	if mode == twofactor.Off {
		return false
	}
	store := twofactor.Instance()
	enabled, err := store.Enabled(ctx, user.ID)
	if err != nil {
		log.Error(ctx, "Two-step verification: could not read the user's setup", "user", user.UserName, err)
		_ = rest.RespondWithError(w, http.StatusInternalServerError, "Unknown error authenticating user. Please try again")
		return true
	}
	if enabled {
		if code == "" {
			respondTwoFactor(w, http.StatusUnauthorized, "code", "Verification code required", nil)
			return true
		}
		if err := store.Verify(ctx, user.ID, code); err != nil {
			if !errors.Is(err, twofactor.ErrInvalidCode) {
				log.Error(ctx, "Two-step verification: could not check the code", "user", user.UserName, err)
			}
			log.Warn(ctx, "Unsuccessful login: wrong verification code", "username", user.UserName)
			respondTwoFactor(w, http.StatusUnauthorized, "code", "Invalid verification code", nil)
			return true
		}
		return false
	}
	if mode == twofactor.Required {
		token, err := setupToken(user)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, "Unknown error authenticating user. Please try again")
			return true
		}
		respondTwoFactor(w, http.StatusUnauthorized, "setup", "Two-step verification must be set up", map[string]any{"setupToken": token})
		return true
	}
	return false
}

// withoutSubsonicToken: a user with two-step verification gets no password-derived Subsonic token in the browser (it
// would open the API without a code); the web UI signs its Subsonic calls with its session instead.
func withoutSubsonicToken(ctx context.Context, user *model.User, payload map[string]any) {
	if enabled, err := twofactor.Instance().Enabled(ctx, user.ID); err == nil && enabled {
		delete(payload, "subsonicSalt")
		delete(payload, "subsonicToken")
	}
}

func setupToken(user *model.User) (string, error) {
	c := auth.Claims{
		Issuer:   consts.JWTIssuer,
		Subject:  user.UserName,
		IssuedAt: time.Now(),
		UserID:   user.ID,
		IsAdmin:  user.IsAdmin,
		Epoch:    user.TokenEpoch,
		Audience: []string{audienceTwoFactorSetup},
	}.WithExpiresAt(time.Now().UTC().Add(setupTokenLifetime))
	_, token, err := auth.TokenAuth.Encode(c.ToMap())
	return token, err
}

type twoFactorRequest struct {
	Token string `json:"token"` // a setup token from the sign-in, or the session's
	Code  string `json:"code"`
}

func readTwoFactorRequest(r *http.Request) (twoFactorRequest, error) {
	var body twoFactorRequest
	err := json.NewDecoder(r.Body).Decode(&body)
	if body.Token == "" {
		body.Token = tokenFromHeader(r)
	}
	return body, err
}

// userForSetup is the user a setup token (or a session token) names.
func userForSetup(ctx context.Context, ds model.DataStore, token string) (*model.User, error) {
	if token == "" {
		return nil, ErrUnauthenticated
	}
	claims, err := auth.Validate(token)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	user, err := ds.User(ctx).FindByUsernameWithPassword(claims.Subject)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if err := auth.CheckClaims(claims, *user, audienceTwoFactorSetup); err != nil {
		return nil, ErrUnauthenticated
	}
	return user, nil
}

// twoFactorSetupBegin starts setting up an authenticator: a new secret, its link and QR code. Replacing one already in
// use takes a code from it (or a recovery code) first.
func twoFactorSetupBegin(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if twofactor.CurrentMode() == twofactor.Off {
			_ = rest.RespondWithError(w, http.StatusForbidden, "Two-step verification is turned off on this server")
			return
		}
		body, err := readTwoFactorRequest(r)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
			return
		}
		user, err := userForSetup(ctx, ds, body.Token)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		store := twofactor.Instance()
		if enabled, err := store.Enabled(ctx, user.ID); err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		} else if enabled && store.Verify(ctx, user.ID, body.Code) != nil {
			respondTwoFactor(w, http.StatusUnauthorized, "code", "Invalid verification code", nil)
			return
		}
		secret, uri, err := store.BeginSetup(ctx, user.ID, user.UserName)
		if err != nil {
			log.Error(ctx, "Two-step verification: could not start the setup", "user", user.UserName, err)
			_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not start the setup")
			return
		}
		qr, err := twofactor.QRCode(uri)
		if err != nil {
			log.Error(ctx, "Two-step verification: could not draw the QR code", err)
		}
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"secret": secret, "uri": uri, "qr": qr, "issuer": twofactor.Issuer, "account": user.UserName})
	}
}

// twoFactorSetupConfirm checks a code from the new authenticator; when it is right, that authenticator is the one in use,
// the user gets their recovery codes, and is signed in (as /auth/login answers).
func twoFactorSetupConfirm(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		body, err := readTwoFactorRequest(r)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
			return
		}
		user, err := userForSetup(ctx, ds, body.Token)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		codes, err := twofactor.Instance().ConfirmSetup(ctx, user.ID, body.Code)
		switch {
		case errors.Is(err, twofactor.ErrInvalidCode):
			respondTwoFactor(w, http.StatusUnauthorized, "setup", "Invalid verification code", nil)
			return
		case errors.Is(err, twofactor.ErrNoSetup):
			_ = rest.RespondWithError(w, http.StatusConflict, "No two-step verification being set up")
			return
		case err != nil:
			log.Error(ctx, "Two-step verification: could not confirm the setup", "user", user.UserName, err)
			_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not confirm the setup")
			return
		}
		log.Info(ctx, "Two-step verification set up", "username", user.UserName)
		token, err := auth.CreateToken(user)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, "Unknown error authenticating user. Please try again")
			return
		}
		payload := buildAuthPayload(user)
		payload["token"] = token
		withoutSubsonicToken(ctx, user, payload)
		payload["recoveryCodes"] = codes
		_ = rest.RespondWithJSON(w, http.StatusOK, payload)
	}
}

// ---------------------------------------------------------------- signed in: the user's own

func twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := request.UserFrom(r.Context())
	st, err := twofactor.Instance().Status(r.Context(), user.ID)
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, st)
}

// withCode runs fn after a code from the user's authenticator (or a recovery code) checks out.
func withCode(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, user model.User)) {
	ctx := r.Context()
	user, _ := request.UserFrom(ctx)
	body, err := readTwoFactorRequest(r)
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
		return
	}
	if err := twofactor.Instance().Verify(ctx, user.ID, body.Code); err != nil {
		respondTwoFactor(w, http.StatusUnauthorized, "code", "Invalid verification code", nil)
		return
	}
	fn(ctx, user)
}

func twoFactorRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	withCode(w, r, func(ctx context.Context, user model.User) {
		codes, err := twofactor.Instance().NewRecoveryCodes(ctx, user.ID)
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
	})
}

func twoFactorDisable(w http.ResponseWriter, r *http.Request) {
	if twofactor.CurrentMode() == twofactor.Required {
		_ = rest.RespondWithError(w, http.StatusForbidden, "Two-step verification is required on this server")
		return
	}
	withCode(w, r, func(ctx context.Context, user model.User) {
		if err := twofactor.Instance().Disable(ctx, user.ID); err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Info(ctx, "Two-step verification turned off", "username", user.UserName)
		w.WriteHeader(http.StatusNoContent)
	})
}

func appPasswordList(w http.ResponseWriter, r *http.Request) {
	user, _ := request.UserFrom(r.Context())
	list, err := twofactor.Instance().AppPasswords(r.Context(), user.ID)
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, list)
}

func appPasswordCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := request.UserFrom(ctx)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
		return
	}
	if len([]rune(body.Name)) > 64 {
		body.Name = string([]rune(body.Name)[:64])
	}
	ap, password, err := twofactor.Instance().CreateAppPassword(ctx, user.ID, body.Name)
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info(ctx, "App password created", "username", user.UserName, "name", ap.Name)
	_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"appPassword": ap, "password": password})
}

func appPasswordDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := request.UserFrom(ctx)
	ok, err := twofactor.Instance().DeleteAppPassword(ctx, user.ID, chi.URLParam(r, "id"))
	switch {
	case err != nil:
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
	case !ok:
		_ = rest.RespondWithError(w, http.StatusNotFound, "No such app password")
	default:
		log.Info(ctx, "App password revoked", "username", user.UserName)
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------------- admins: someone else's

func adminOnlyHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if user, ok := request.UserFrom(r.Context()); !ok || !user.IsAdmin {
			_ = rest.RespondWithError(w, http.StatusForbidden, "Only admins can do this")
			return
		}
		next(w, r)
	}
}

func twoFactorUserStatus(w http.ResponseWriter, r *http.Request) {
	st, err := twofactor.Instance().Status(r.Context(), chi.URLParam(r, "userId"))
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, st)
}

// twoFactorUserReset removes someone's two-step verification (a lost phone and no recovery codes): they set it up again
// at their next sign-in. Their app passwords keep working.
func twoFactorUserReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	admin, _ := request.UserFrom(ctx)
	userID := chi.URLParam(r, "userId")
	if err := twofactor.Instance().Disable(ctx, userID); err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Warn(ctx, "Two-step verification reset by an admin", "admin", admin.UserName, "userId", userID)
	w.WriteHeader(http.StatusNoContent)
}

// mountTwoFactorRoutes adds the routes to /auth: the setup ones (rate-limited as the sign-in is), and the signed-in ones.
func mountTwoFactorRoutes(r chi.Router, ds model.DataStore, limit func(http.Handler) http.Handler) {
	r.With(limit).Post("/2fa/setup", twoFactorSetupBegin(ds))
	r.With(limit).Post("/2fa/confirm", twoFactorSetupConfirm(ds))
	r.Group(func(r chi.Router) {
		r.Use(Authenticator(ds))
		r.Get("/2fa", twoFactorStatus)
		r.With(limit).Post("/2fa/recovery-codes", twoFactorRecoveryCodes)
		r.With(limit).Post("/2fa/disable", twoFactorDisable)
		r.Get("/app-passwords", appPasswordList)
		r.Post("/app-passwords", appPasswordCreate)
		r.Delete("/app-passwords/{id}", appPasswordDelete)
		r.Get("/2fa/users/{userId}", adminOnlyHandler(twoFactorUserStatus))
		r.Post("/2fa/users/{userId}/reset", adminOnlyHandler(twoFactorUserReset))
	})
}
