package twofactor

import (
	"context"
	"database/sql"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("codes", func() {
	// RFC 6238, appendix B: the SHA-1 secret "12345678901234567890", its 8-digit codes cut to the 6 apps show
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	DescribeTable("match the RFC's test vectors",
		func(unix int64, want string) {
			Expect(Code(secret, time.Unix(unix, 0))).To(Equal(want))
		},
		Entry("59", int64(59), "287082"),
		Entry("1111111109", int64(1111111109), "081804"),
		Entry("1111111111", int64(1111111111), "050471"),
		Entry("1234567890", int64(1234567890), "005924"),
		Entry("2000000000", int64(2000000000), "279037"),
		Entry("20000000000", int64(20000000000), "353130"),
	)

	It("accepts the step before and after now, not two away", func() {
		now := time.Unix(1_800_000_000, 0)
		for d, ok := range map[int64]bool{-60: false, -30: true, 0: true, 30: true, 60: false} {
			c, _ := Code(secret, now.Add(time.Duration(d)*time.Second))
			Expect(matchStep(secret, c, now) >= 0).To(Equal(ok), fmt.Sprint(d))
		}
	})

	It("takes the digits as people type them", func() {
		now := time.Unix(1_800_000_000, 0)
		c, _ := Code(secret, now)
		Expect(matchStep(secret, c[:3]+" "+c[3:], now)).To(BeNumerically(">=", 0))
		Expect(matchStep(secret, "12345", now)).To(Equal(int64(-1)))
		Expect(matchStep(secret, "abcdef", now)).To(Equal(int64(-1)))
	})

	It("links authenticator apps to the account", func() {
		uri := URI("Sonus", "alice", "ABC")
		Expect(uri).To(HavePrefix("otpauth://totp/Sonus:alice?"))
		Expect(uri).To(ContainSubstring("secret=ABC"))
		Expect(uri).To(ContainSubstring("issuer=Sonus"))
		qr, err := QRCode(uri)
		Expect(err).ToNot(HaveOccurred())
		Expect(qr).To(HavePrefix("data:image/png;base64,"))
	})

	It("makes random strings of the readable alphabet, grouped", func() {
		s, err := randomString(16, 4)
		Expect(err).ToNot(HaveOccurred())
		Expect(s).To(MatchRegexp(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`))
		Expect(s).ToNot(ContainSubstring("0"))
		Expect(s).ToNot(ContainSubstring("l"))
	})
})

var _ = Describe("Store", func() {
	var (
		ctx   context.Context
		store *Store
		now   time.Time
		d     *sql.DB
	)

	codeNow := func(secret string) string { c, _ := Code(secret, now); return c }

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.Sonus.TwoFactor = "required"
		ctx = context.Background()
		var err error
		name := fmt.Sprintf("twofactor_%d", time.Now().UnixNano())
		sql.Register(name, &sqlite3.SQLiteDriver{})
		d, err = sql.Open(name, "file::memory:?_foreign_keys=on")
		Expect(err).ToNot(HaveOccurred())
		d.SetMaxOpenConns(1)
		DeferCleanup(d.Close)
		_, err = d.Exec(`CREATE TABLE user (id TEXT PRIMARY KEY, user_name TEXT)`)
		Expect(err).ToNot(HaveOccurred())
		_, err = d.Exec(`INSERT INTO user (id, user_name) VALUES ('u1', 'alice'), ('u2', 'bob')`)
		Expect(err).ToNot(HaveOccurred())
		store = New(d)
		now = time.Unix(1_800_000_000, 0)
		store.now = func() time.Time { return now }
	})

	setUp := func(user string) (string, []string) {
		secret, uri, err := store.BeginSetup(ctx, user, "alice")
		Expect(err).ToNot(HaveOccurred())
		Expect(uri).To(ContainSubstring(secret))
		codes, err := store.ConfirmSetup(ctx, user, codeNow(secret))
		Expect(err).ToNot(HaveOccurred())
		return secret, codes
	}

	It("is off until a code from the new secret is confirmed", func() {
		secret, _, err := store.BeginSetup(ctx, "u1", "alice")
		Expect(err).ToNot(HaveOccurred())
		Expect(store.Enabled(ctx, "u1")).To(BeFalse())
		_, err = store.ConfirmSetup(ctx, "u1", "000000")
		Expect(err).To(MatchError(ErrInvalidCode))
		codes, err := store.ConfirmSetup(ctx, "u1", codeNow(secret))
		Expect(err).ToNot(HaveOccurred())
		Expect(codes).To(HaveLen(10))
		Expect(store.Enabled(ctx, "u1")).To(BeTrue())
		Expect(store.Enabled(ctx, "u2")).To(BeFalse())
		st, err := store.Status(ctx, "u1")
		Expect(err).ToNot(HaveOccurred())
		Expect(st.Enabled).To(BeTrue())
		Expect(st.RecoveryCodesLeft).To(Equal(10))
	})

	It("keeps the secret sealed in the database", func() {
		secret, _ := setUp("u1")
		var sealed string
		Expect(d.QueryRow(`SELECT secret FROM sonus_two_factor WHERE user_id = 'u1'`).Scan(&sealed)).To(Succeed())
		Expect(sealed).ToNot(ContainSubstring(secret))
	})

	It("takes each authenticator code once", func() {
		secret, _ := setUp("u1")
		now = now.Add(30 * time.Second)
		c := codeNow(secret)
		Expect(store.Verify(ctx, "u1", c)).To(Succeed())
		Expect(store.Verify(ctx, "u1", c)).To(MatchError(ErrInvalidCode))
		Expect(store.Verify(ctx, "u1", "123456")).To(MatchError(ErrInvalidCode))
		now = now.Add(30 * time.Second)
		Expect(store.Verify(ctx, "u1", codeNow(secret))).To(Succeed())
	})

	It("takes each recovery code once, as typed or in capitals", func() {
		_, codes := setUp("u1")
		Expect(store.Verify(ctx, "u1", strings.ToUpper(codes[0]))).To(Succeed())
		Expect(store.Verify(ctx, "u1", codes[0])).To(MatchError(ErrInvalidCode))
		Expect(store.Verify(ctx, "u2", codes[1])).To(MatchError(ErrInvalidCode))
		st, _ := store.Status(ctx, "u1")
		Expect(st.RecoveryCodesLeft).To(Equal(9))
		fresh, err := store.NewRecoveryCodes(ctx, "u1")
		Expect(err).ToNot(HaveOccurred())
		Expect(store.Verify(ctx, "u1", codes[1])).To(MatchError(ErrInvalidCode))
		Expect(store.Verify(ctx, "u1", fresh[0])).To(Succeed())
	})

	It("keeps the old authenticator until a new one is confirmed", func() {
		old, _ := setUp("u1")
		fresh, _, err := store.BeginSetup(ctx, "u1", "alice")
		Expect(err).ToNot(HaveOccurred())
		now = now.Add(30 * time.Second)
		Expect(store.Verify(ctx, "u1", codeNow(old))).To(Succeed())
		now = now.Add(30 * time.Second)
		_, err = store.ConfirmSetup(ctx, "u1", codeNow(fresh))
		Expect(err).ToNot(HaveOccurred())
		now = now.Add(30 * time.Second)
		Expect(store.Verify(ctx, "u1", codeNow(old))).To(MatchError(ErrInvalidCode))
		Expect(store.Verify(ctx, "u1", codeNow(fresh))).To(Succeed())
	})

	It("is not enforced when the policy is off, and can be removed", func() {
		setUp("u1")
		conf.Server.Sonus.TwoFactor = "off"
		Expect(store.Enabled(ctx, "u1")).To(BeFalse())
		conf.Server.Sonus.TwoFactor = "optional"
		Expect(store.Enabled(ctx, "u1")).To(BeTrue())
		Expect(store.Disable(ctx, "u1")).To(Succeed())
		Expect(store.Enabled(ctx, "u1")).To(BeFalse())
		st, _ := store.Status(ctx, "u1")
		Expect(st.RecoveryCodesLeft).To(Equal(0))
	})

	Describe("app passwords", func() {
		It("match as shown or without dashes, only for their user, and note the client", func() {
			ap, password, err := store.CreateAppPassword(ctx, "u1", " 书房电脑 ")
			Expect(err).ToNot(HaveOccurred())
			Expect(ap.Name).To(Equal("书房电脑"))
			Expect(password).To(MatchRegexp(`^[a-z2-9]{4}(-[a-z2-9]{4}){3}$`))
			is := func(want string) func(string) bool { return func(p string) bool { return p == want } }
			Expect(store.MatchAppPassword(ctx, "u1", is(password), "Sonus (PC)")).To(BeTrue())
			Expect(store.MatchAppPassword(ctx, "u1", is(strings.ReplaceAll(password, "-", "")), "Symfonium")).To(BeTrue())
			Expect(store.MatchAppPassword(ctx, "u2", is(password), "Sonus (PC)")).To(BeFalse())
			Expect(store.MatchAppPassword(ctx, "u1", is("wrong"), "Sonus (PC)")).To(BeFalse())
			list, err := store.AppPasswords(ctx, "u1")
			Expect(err).ToNot(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].LastUsedBy).To(Equal("Symfonium"))
			Expect(list[0].LastUsedAt).ToNot(BeNil())
		})

		It("stop working when revoked, and only their user can revoke them", func() {
			ap, password, _ := store.CreateAppPassword(ctx, "u1", "phone")
			Expect(store.DeleteAppPassword(ctx, "u2", ap.ID)).To(BeFalse())
			Expect(store.DeleteAppPassword(ctx, "u1", ap.ID)).To(BeTrue())
			Expect(store.MatchAppPassword(ctx, "u1", func(p string) bool { return p == password }, "x")).To(BeFalse())
		})

		It("go with their user", func() {
			store.CreateAppPassword(ctx, "u1", "phone")
			setUp("u1")
			_, err := d.Exec(`DELETE FROM user WHERE id = 'u1'`)
			Expect(err).ToNot(HaveOccurred())
			list, _ := store.AppPasswords(ctx, "u1")
			Expect(list).To(BeEmpty())
			Expect(store.Enabled(ctx, "u1")).To(BeFalse())
		})
	})
})
