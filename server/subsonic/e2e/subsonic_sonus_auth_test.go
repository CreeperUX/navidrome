package e2e

import (
	"crypto/md5"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/twofactor"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Sonus: with two-step verification set up, the Subsonic API takes app passwords, not the account password.
var _ = Describe("Two-step verification in the Subsonic API (Sonus)", func() {
	BeforeEach(func() {
		setupTestDB()
	})

	// ping signed by regularUser with the given auth parameters
	ping := func(auth ...string) *responses.Subsonic {
		q := url.Values{}
		q.Add("u", regularUser.UserName)
		q.Add("v", "1.16.1")
		q.Add("c", "Sonus (PC)")
		q.Add("f", "json")
		for i := 0; i < len(auth); i += 2 {
			q.Add(auth[i], auth[i+1])
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/ping?"+q.Encode(), nil))
		return parseJSONResponse(w)
	}
	tokenFor := func(password, salt string) string { return fmt.Sprintf("%x", md5.Sum([]byte(password+salt))) }
	setUp := func() {
		store := twofactor.Instance()
		secret, _, err := store.BeginSetup(ctx, regularUser.ID, regularUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		code, _ := twofactor.Code(secret, time.Now())
		_, err = store.ConfirmSetup(ctx, regularUser.ID, code)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = store.Disable(ctx, regularUser.ID) })
	}

	It("takes the account password while it is not set up", func() {
		Expect(ping("p", "password").Status).To(Equal(responses.StatusOK))
		Expect(ping("t", tokenFor("password", "abc"), "s", "abc").Status).To(Equal(responses.StatusOK))
	})

	It("refuses the account password once it is set up, in every form", func() {
		setUp()
		for _, auth := range [][]string{
			{"p", "password"},
			{"p", "enc:" + fmt.Sprintf("%x", "password")},
			{"t", tokenFor("password", "abc"), "s", "abc"},
		} {
			resp := ping(auth...)
			Expect(resp.Status).To(Equal(responses.StatusFailed), "%v", auth)
			Expect(resp.Error.Code).To(Equal(responses.ErrorAuthenticationFail))
		}
		// other users are not affected
		Expect(doReq("ping").Status).To(Equal(responses.StatusOK))
	})

	It("takes an app password as p= or t= with s=, with or without its dashes, until it is revoked", func() {
		setUp()
		store := twofactor.Instance()
		ap, password, err := store.CreateAppPassword(ctx, regularUser.ID, "PC")
		Expect(err).ToNot(HaveOccurred())
		plain := strings.ReplaceAll(password, "-", "")
		Expect(ping("p", password).Status).To(Equal(responses.StatusOK))
		Expect(ping("p", plain).Status).To(Equal(responses.StatusOK))
		Expect(ping("p", "enc:"+fmt.Sprintf("%x", password)).Status).To(Equal(responses.StatusOK))
		Expect(ping("t", tokenFor(password, "s1"), "s", "s1").Status).To(Equal(responses.StatusOK))
		Expect(ping("t", tokenFor(plain, "s2"), "s", "s2").Status).To(Equal(responses.StatusOK))
		Expect(ping("p", "wrong-pass-word").Status).To(Equal(responses.StatusFailed))

		list, err := store.AppPasswords(ctx, regularUser.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(list[0].LastUsedBy).To(Equal("Sonus (PC)"))

		Expect(store.DeleteAppPassword(ctx, regularUser.ID, ap.ID)).To(BeTrue())
		Expect(ping("p", password).Status).To(Equal(responses.StatusFailed))
	})

	Describe("API keys (OpenSubsonic apiKeyAuthentication)", func() {
		// a request signed by an API key alone: no u=
		withKey := func(endpoint string, params ...string) *responses.Subsonic {
			q := url.Values{}
			q.Add("v", "1.16.1")
			q.Add("c", "Sonus (PC)")
			q.Add("f", "json")
			for i := 0; i < len(params); i += 2 {
				q.Add(params[i], params[i+1])
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+endpoint+"?"+q.Encode(), nil))
			return parseJSONResponse(w)
		}

		It("signs requests with an app password as apiKey, and tokenInfo names its user", func() {
			_, key, err := twofactor.Instance().CreateAPIKey(ctx, regularUser.ID, "Sonus · PC")
			Expect(err).ToNot(HaveOccurred())
			Expect(withKey("ping", "apiKey", key).Status).To(Equal(responses.StatusOK))
			info := withKey("tokenInfo", "apiKey", key)
			Expect(info.Status).To(Equal(responses.StatusOK))
			Expect(info.TokenInfo.Username).To(Equal(regularUser.UserName))
			// what the key can see is its user's
			Expect(withKey("getMusicFolders", "apiKey", key).Status).To(Equal(responses.StatusOK))
		})

		It("works for a user with two-step verification too", func() {
			setUp()
			_, password, _ := twofactor.Instance().CreateAppPassword(ctx, regularUser.ID, "phone")
			Expect(withKey("ping", "apiKey", password).Status).To(Equal(responses.StatusOK))
		})

		It("answers 43 when other credentials come along, and 44 for a key that is no one's", func() {
			_, key, _ := twofactor.Instance().CreateAPIKey(ctx, regularUser.ID, "x")
			for _, other := range [][]string{{"u", regularUser.UserName}, {"p", "password"}, {"t", "abc", "s", "abc"}} {
				resp := withKey("ping", append([]string{"apiKey", key}, other...)...)
				Expect(resp.Status).To(Equal(responses.StatusFailed))
				Expect(resp.Error.Code).To(Equal(int32(43)), "%v", other)
			}
			resp := withKey("ping", "apiKey", "no-such-key")
			Expect(resp.Status).To(Equal(responses.StatusFailed))
			Expect(resp.Error.Code).To(Equal(int32(44)))
			Expect(twofactor.Instance().RevokeAPIKey(ctx, key)).To(BeTrue())
			Expect(withKey("ping", "apiKey", key).Error.Code).To(Equal(int32(44)))
		})

		It("is announced with the browser sign-in", func() {
			resp := doReq("getOpenSubsonicExtensions")
			Expect(*resp.OpenSubsonicExtensions).To(ContainElements(
				responses.OpenSubsonicExtension{Name: "apiKeyAuthentication", Versions: []int32{1}},
				responses.OpenSubsonicExtension{Name: "sonusDeviceLogin", Versions: []int32{1}},
			))
		})
	})
})
