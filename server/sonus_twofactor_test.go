package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Two-step verification at sign-in (Sonus)", Ordered, func() {
	var (
		ds     model.DataStore
		user   model.User
		router chi.Router
		ctx    = context.Background()
	)

	BeforeAll(func() {
		db.Init(ctx)
		ds = &tests.MockDataStore{RealDS: persistence.New(db.Db())}
		auth.Init(ds)
	})

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.Sonus.TwoFactor = "required"
		user = model.User{ID: id.NewRandom(), UserName: "u" + id.NewRandom()[:8], Name: "Alice", NewPassword: "secret"}
		Expect(ds.User(ctx).Put(&user)).To(Succeed())
		router = chi.NewRouter()
		router.Use(JWTVerifier)
		router.Post("/login", login(ds))
		mountTwoFactorRoutes(router, ds, func(next http.Handler) http.Handler { return next })
	})

	call := func(method, path string, body any, token string) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		if token != "" {
			req.Header.Set("X-ND-Authorization", "Bearer "+token)
		}
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		parsed := map[string]any{}
		_ = json.Unmarshal(resp.Body.Bytes(), &parsed)
		return resp.Code, parsed
	}
	signIn := func(otp string) (int, map[string]any) {
		return call("POST", "/login", map[string]string{"username": user.UserName, "password": "secret", "otp": otp}, "")
	}
	// setUp goes through the first sign-in: the setup token, the secret, the confirmed code
	setUp := func() (secret string, recovery []any) {
		code, body := signIn("")
		Expect(code).To(Equal(http.StatusUnauthorized))
		Expect(body["twoFactor"]).To(Equal("setup"))
		token := body["setupToken"].(string)
		code, body = call("POST", "/2fa/setup", map[string]string{"token": token}, "")
		Expect(code).To(Equal(http.StatusOK))
		secret = body["secret"].(string)
		Expect(body["qr"]).To(HavePrefix("data:image/png;base64,"))
		now, _ := twofactor.Code(secret, time.Now())
		code, body = call("POST", "/2fa/confirm", map[string]string{"token": token, "code": now}, "")
		Expect(code).To(Equal(http.StatusOK), "%v", body)
		Expect(body["token"]).ToNot(BeEmpty())
		Expect(body).ToNot(HaveKey("subsonicToken"))
		return secret, body["recoveryCodes"].([]any)
	}
	nextCode := func(secret string) string {
		// a code not used yet: the following step's (accepted, as clocks drift)
		c, _ := twofactor.Code(secret, time.Now().Add(30*time.Second))
		return c
	}

	It("has the user set it up at the first sign-in, then signs them in with recovery codes to keep", func() {
		_, recovery := setUp()
		Expect(recovery).To(HaveLen(10))
		Expect(twofactor.Instance().Enabled(ctx, user.ID)).To(BeTrue())
	})

	It("gives a setup token no API takes, and no session before the code", func() {
		_, body := signIn("")
		Expect(body).ToNot(HaveKey("token"))
		claims, err := auth.Validate(body["setupToken"].(string))
		Expect(err).ToNot(HaveOccurred())
		for _, audience := range []string{auth.AudienceNative, auth.AudienceSubsonic, auth.AudienceJellyfin} {
			Expect(auth.CheckClaims(claims, user, audience)).To(MatchError(auth.ErrWrongAudience))
		}
		code, _ := call("POST", "/2fa/confirm", map[string]string{"token": body["setupToken"].(string), "code": "123456"}, "")
		Expect(code).To(Equal(http.StatusConflict)) // nothing being set up yet
		code, _ = call("GET", "/2fa", nil, body["setupToken"].(string))
		Expect(code).To(Equal(http.StatusUnauthorized))
	})

	It("then wants a code at every sign-in, each code once, or a recovery code", func() {
		secret, recovery := setUp()
		code, body := signIn("")
		Expect(code).To(Equal(http.StatusUnauthorized))
		Expect(body["twoFactor"]).To(Equal("code"))
		code, _ = signIn("000000")
		Expect(code).To(Equal(http.StatusUnauthorized))
		next := nextCode(secret)
		code, body = signIn(next)
		Expect(code).To(Equal(http.StatusOK))
		Expect(body["token"]).ToNot(BeEmpty())
		Expect(body).ToNot(HaveKey("subsonicToken"))
		code, _ = signIn(next)
		Expect(code).To(Equal(http.StatusUnauthorized))
		code, _ = signIn(recovery[0].(string))
		Expect(code).To(Equal(http.StatusOK))
		code, _ = signIn(recovery[0].(string))
		Expect(code).To(Equal(http.StatusUnauthorized))
	})

	It("refuses the wrong password before anything else", func() {
		setUp()
		code, body := call("POST", "/login", map[string]string{"username": user.UserName, "password": "nope"}, "")
		Expect(code).To(Equal(http.StatusUnauthorized))
		Expect(body).ToNot(HaveKey("twoFactor"))
	})

	It("signs in as upstream does when it is off, or optional and not set up", func() {
		conf.Server.Sonus.TwoFactor = "optional"
		code, body := signIn("")
		Expect(code).To(Equal(http.StatusOK))
		Expect(body["subsonicToken"]).ToNot(BeEmpty())
		conf.Server.Sonus.TwoFactor = "required"
		setUp()
		conf.Server.Sonus.TwoFactor = "off"
		code, body = signIn("")
		Expect(code).To(Equal(http.StatusOK))
		Expect(body["token"]).ToNot(BeEmpty())
	})

	It("lets a signed-in user manage app passwords and recovery codes", func() {
		secret, recovery := setUp()
		_, body := signIn(nextCode(secret))
		session := body["token"].(string)
		code, body := call("POST", "/app-passwords", map[string]string{"name": "书房电脑"}, session)
		Expect(code).To(Equal(http.StatusOK))
		password := body["password"].(string)
		Expect(password).To(MatchRegexp(`^[a-z2-9]{4}(-[a-z2-9]{4}){3}$`))
		appID := body["appPassword"].(map[string]any)["id"].(string)
		code, list := call("GET", "/app-passwords", nil, session)
		Expect(code).To(Equal(http.StatusOK))
		_ = list
		code, _ = call("POST", "/2fa/recovery-codes", map[string]string{"code": "000000"}, session)
		Expect(code).To(Equal(http.StatusUnauthorized))
		c := recovery[0].(string) // both usable authenticator codes are spent: a recovery code checks out too
		code, body = call("POST", "/2fa/recovery-codes", map[string]string{"code": c}, session)
		Expect(code).To(Equal(http.StatusOK))
		Expect(body["recoveryCodes"]).To(HaveLen(10))
		code, _ = call("POST", "/2fa/disable", map[string]string{"code": c}, session)
		Expect(code).To(Equal(http.StatusForbidden)) // required here
		code, _ = call("DELETE", "/app-passwords/"+appID, nil, session)
		Expect(code).To(Equal(http.StatusNoContent))
		code, _ = call("DELETE", "/app-passwords/"+appID, nil, session)
		Expect(code).To(Equal(http.StatusNotFound))
		code, _ = call("POST", "/2fa/users/"+user.ID+"/reset", nil, session)
		Expect(code).To(Equal(http.StatusForbidden)) // not an admin
	})
	Describe("browser sign-in for apps", func() {
		session := func() string {
			conf.Server.Sonus.TwoFactor = "off"
			_, body := signIn("")
			return body["token"].(string)
		}
		start := func() map[string]any {
			code, body := call("POST", "/device/start", map[string]string{"client": "Sonus", "device": "书房电脑"}, "")
			Expect(code).To(Equal(http.StatusOK))
			Expect(body["verificationPath"]).To(Equal("/app/#/device?code=" + body["userCode"].(string)))
			return body
		}
		poll := func(deviceCode string) map[string]any {
			_, body := call("POST", "/device/poll", map[string]string{"deviceCode": deviceCode}, "")
			return body
		}

		It("hands the app an API key once its user allows it, only once", func() {
			s := start()
			Expect(poll(s["deviceCode"].(string))["status"]).To(Equal("pending"))
			token := session()
			code, info := call("GET", "/device/"+strings.ToLower(s["userCode"].(string)), nil, token)
			Expect(code).To(Equal(http.StatusOK))
			Expect(info["client"]).To(Equal("Sonus"))
			Expect(info["device"]).To(Equal("书房电脑"))
			code, _ = call("POST", "/device/"+s["userCode"].(string)+"/approve", nil, token)
			Expect(code).To(Equal(http.StatusNoContent))
			got := poll(s["deviceCode"].(string))
			Expect(got["status"]).To(Equal("approved"))
			Expect(got["username"]).To(Equal(user.UserName))
			key := got["apiKey"].(string)
			Expect(twofactor.Instance().UserForAPIKey(ctx, key, "Sonus")).To(Equal(user.ID))
			Expect(poll(s["deviceCode"].(string))["status"]).To(Equal("expired")) // handed over once
			list, _ := twofactor.Instance().AppPasswords(ctx, user.ID)
			Expect(list[0].Name).To(Equal("Sonus · 书房电脑"))
			// the app gives it back when it signs out
			code, _ = call("POST", "/device/revoke", map[string]string{"apiKey": key}, "")
			Expect(code).To(Equal(http.StatusNoContent))
			Expect(twofactor.Instance().UserForAPIKey(ctx, key, "Sonus")).To(BeEmpty())
		})

		It("tells the app when its user refuses, and needs a signed-in user to decide", func() {
			s := start()
			code, _ := call("POST", "/device/"+s["userCode"].(string)+"/approve", nil, "")
			Expect(code).To(Equal(http.StatusUnauthorized))
			token := session()
			code, _ = call("POST", "/device/"+s["userCode"].(string)+"/deny", nil, token)
			Expect(code).To(Equal(http.StatusNoContent))
			Expect(poll(s["deviceCode"].(string))["status"]).To(Equal("denied"))
			code, _ = call("POST", "/device/"+s["userCode"].(string)+"/approve", nil, token)
			Expect(code).To(Equal(http.StatusNotFound))
		})

		It("forgets sign-ins after ten minutes", func() {
			s := start()
			pendingLogins.mu.Lock()
			pendingLogins.byDevice[s["deviceCode"].(string)].created = time.Now().Add(-11 * time.Minute)
			pendingLogins.mu.Unlock()
			Expect(poll(s["deviceCode"].(string))["status"]).To(Equal("expired"))
			code, _ := call("GET", "/device/"+s["userCode"].(string), nil, session())
			Expect(code).To(Equal(http.StatusNotFound))
		})
	})
})
