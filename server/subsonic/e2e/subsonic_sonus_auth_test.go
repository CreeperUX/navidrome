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
})
