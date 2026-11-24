package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// The browser sign-in for apps (Sonus), as OAuth's device authorization grant (RFC 8628) has it. The app asks for a
// sign-in and gets a device code it keeps, and a short user code to show; it opens the approval page in the browser
// (#/device?code=…), where the user signs in (with two-step verification) and, seeing the same code as in the app,
// allows it. The app, polling meanwhile with its device code, then gets an API key: an app password made for it,
// named after the app and the device, listed and revocable as the others. The app never sees the account password.
// Pending sign-ins are kept in memory: a restart only means asking again.

// DeviceLoginExtension is the OpenSubsonic extension announcing it.
const DeviceLoginExtension = "sonusDeviceLogin"

const (
	deviceLoginLifetime = 10 * time.Minute
	deviceLoginInterval = 2 // seconds between polls
	deviceLoginMax      = 200
	// letters that make no words and are not mistaken for each other
	userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
)

type deviceLogin struct {
	deviceCode, userCode string
	client, device       string
	created              time.Time
	status               string // pending, approved, denied
	apiKey, username     string
}

type deviceLogins struct {
	mu       sync.Mutex
	byDevice map[string]*deviceLogin
	byUser   map[string]*deviceLogin
	now      func() time.Time
}

var pendingLogins = newDeviceLogins()

func newDeviceLogins() *deviceLogins {
	return &deviceLogins{byDevice: map[string]*deviceLogin{}, byUser: map[string]*deviceLogin{}, now: time.Now}
}

// sweep drops expired sign-ins; the caller holds mu.
func (d *deviceLogins) sweep() {
	for code, l := range d.byDevice {
		if d.now().Sub(l.created) > deviceLoginLifetime {
			delete(d.byDevice, code)
			delete(d.byUser, l.userCode)
		}
	}
}

func (d *deviceLogins) drop(l *deviceLogin) {
	delete(d.byDevice, l.deviceCode)
	delete(d.byUser, l.userCode)
}

func randomUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for i, v := range b {
		if i == 4 {
			sb.WriteByte('-')
		}
		sb.WriteByte(userCodeAlphabet[int(v)%len(userCodeAlphabet)]) // 256 % 20 = 16: a slight lean, harmless for a code shown to compare
	}
	return sb.String(), nil
}

// normalUserCode takes the code as typed: any case, with or without its dash.
func normalUserCode(code string) string {
	c := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(c) != 8 {
		return ""
	}
	return c[:4] + "-" + c[4:]
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// deviceLoginStart: an app asks to sign in ({client, device}).
func deviceLoginStart(w http.ResponseWriter, r *http.Request) {
	var body struct{ Client, Device string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
		return
	}
	deviceBytes := make([]byte, 32)
	if _, err := rand.Read(deviceBytes); err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d := pendingLogins
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweep()
	if len(d.byDevice) >= deviceLoginMax {
		_ = rest.RespondWithError(w, http.StatusTooManyRequests, "Too many sign-ins waiting; try again in a few minutes")
		return
	}
	var userCode string
	for userCode == "" || d.byUser[userCode] != nil {
		code, err := randomUserCode()
		if err != nil {
			_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		userCode = code
	}
	l := &deviceLogin{
		deviceCode: hex.EncodeToString(deviceBytes),
		userCode:   userCode,
		client:     clip(body.Client, 40),
		device:     clip(body.Device, 64),
		created:    d.now(),
		status:     "pending",
	}
	if l.client == "" {
		l.client = "App"
	}
	d.byDevice[l.deviceCode] = l
	d.byUser[l.userCode] = l
	_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{
		"deviceCode":       l.deviceCode,
		"userCode":         l.userCode,
		"verificationPath": path.Join(conf.Server.BasePath, "/app/") + "/#/device?code=" + l.userCode,
		"expiresIn":        int(deviceLoginLifetime.Seconds()),
		"interval":         deviceLoginInterval,
	})
}

// deviceLoginPoll: the app asks how its sign-in stands ({deviceCode}); the key is handed over once.
func deviceLoginPoll(w http.ResponseWriter, r *http.Request) {
	var body struct{ DeviceCode string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
		return
	}
	d := pendingLogins
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweep()
	l := d.byDevice[body.DeviceCode]
	switch {
	case l == nil:
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"status": "expired"})
	case l.status == "approved":
		d.drop(l)
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"status": "approved", "apiKey": l.apiKey, "username": l.username})
	case l.status == "denied":
		d.drop(l)
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"status": "denied"})
	default:
		_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{"status": "pending", "interval": deviceLoginInterval})
	}
}

// pendingFor is the sign-in waiting under a user code; the caller holds mu.
func (d *deviceLogins) pendingFor(code string) *deviceLogin {
	d.sweep()
	if l := d.byUser[normalUserCode(code)]; l != nil && l.status == "pending" {
		return l
	}
	return nil
}

// deviceLoginInfo: what the approval page shows, for the signed-in user.
func deviceLoginInfo(w http.ResponseWriter, r *http.Request) {
	d := pendingLogins
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.pendingFor(chi.URLParam(r, "code"))
	if l == nil {
		_ = rest.RespondWithError(w, http.StatusNotFound, "No sign-in waiting for this code: it may have expired")
		return
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, map[string]any{
		"userCode":  l.userCode,
		"client":    l.client,
		"device":    l.device,
		"expiresIn": int((deviceLoginLifetime - d.now().Sub(l.created)).Seconds()),
	})
}

// deviceLoginDecide: the signed-in user allows the app (an API key made for it) or refuses it.
func deviceLoginDecide(allow bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user, _ := request.UserFrom(ctx)
		d := pendingLogins
		d.mu.Lock()
		defer d.mu.Unlock()
		l := d.pendingFor(chi.URLParam(r, "code"))
		if l == nil {
			_ = rest.RespondWithError(w, http.StatusNotFound, "No sign-in waiting for this code: it may have expired")
			return
		}
		if !allow {
			l.status = "denied"
			log.Info(ctx, "Browser sign-in refused", "username", user.UserName, "client", l.client, "device", l.device)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		name := l.client
		if l.device != "" {
			name += " · " + l.device
		}
		_, key, err := twofactor.Instance().CreateAPIKey(ctx, user.ID, name)
		if err != nil {
			log.Error(ctx, "Browser sign-in: could not make the API key", "username", user.UserName, err)
			_ = rest.RespondWithError(w, http.StatusInternalServerError, "Could not sign the app in")
			return
		}
		l.status, l.apiKey, l.username = "approved", key, user.UserName
		log.Info(ctx, "Browser sign-in allowed", "username", user.UserName, "client", l.client, "device", l.device)
		w.WriteHeader(http.StatusNoContent)
	}
}

// deviceLoginRevoke: an app signing out gives its own API key back ({apiKey}).
func deviceLoginRevoke(w http.ResponseWriter, r *http.Request) {
	var body struct{ APIKey string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.APIKey == "" {
		_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, "invalid request payload")
		return
	}
	ok, err := twofactor.Instance().RevokeAPIKey(r.Context(), body.APIKey)
	switch {
	case err != nil:
		_ = rest.RespondWithError(w, http.StatusInternalServerError, err.Error())
	case !ok:
		_ = rest.RespondWithError(w, http.StatusNotFound, "No such API key")
	default:
		log.Info(r.Context(), "API key given back by its app")
		w.WriteHeader(http.StatusNoContent)
	}
}

// mountDeviceLoginRoutes adds the browser sign-in's routes to /auth: the app's (rate-limited as the sign-in is) and the
// approval page's (signed in).
func mountDeviceLoginRoutes(r chi.Router, ds model.DataStore, limit func(http.Handler) http.Handler) {
	r.With(limit).Post("/device/start", deviceLoginStart)
	r.Post("/device/poll", deviceLoginPoll)
	r.With(limit).Post("/device/revoke", deviceLoginRevoke)
	r.Group(func(r chi.Router) {
		r.Use(Authenticator(ds))
		r.Get("/device/{code}", deviceLoginInfo)
		r.Post("/device/{code}/approve", deviceLoginDecide(true))
		r.Post("/device/{code}/deny", deviceLoginDecide(false))
	})
}
