// Package twofactor is this fork's two-step verification (Sonus): an authenticator code at the web sign-in, recovery
// codes for a lost phone, and app passwords for the clients that cannot ask for a code (the Subsonic and Jellyfin APIs).
// With it on, the account password alone opens nothing: the web UI wants a code too, the APIs want an app password.
//
// Its tables (sonus_*, db.SonusSchema) are created outside upstream's migrations, so upstream's migrations stay as they are and
// switching back to upstream Navidrome leaves them unused. Secrets are encrypted as Navidrome encrypts passwords
// (PasswordEncryptionKey); recovery codes are kept only as hashes.
package twofactor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/utils"
)

// Issuer names the account in authenticator apps.
const Issuer = "Sonus"

// Mode is the server's policy (Sonus.TwoFactor).
type Mode string

const (
	Required Mode = "required" // everyone, asked to set it up at the next sign-in
	Optional Mode = "optional" // each user decides
	Off      Mode = "off"      // neither asked nor checked
)

// CurrentMode is the configured policy; anything unknown is Required.
func CurrentMode() Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(conf.Server.Sonus.TwoFactor))) {
	case Optional:
		return Optional
	case Off:
		return Off
	default:
		return Required
	}
}

var (
	ErrInvalidCode = errors.New("invalid verification code")
	ErrNoSetup     = errors.New("no two-step verification being set up")
	ErrUnreadable  = errors.New("two-step verification data cannot be read: was PasswordEncryptionKey changed?")
)

// Status is what a user (or an admin) sees of their two-step verification.
type Status struct {
	Mode              Mode       `json:"mode"`
	Enabled           bool       `json:"enabled"`
	EnabledAt         *time.Time `json:"enabledAt,omitempty"`
	RecoveryCodesLeft int        `json:"recoveryCodesLeft"`
}

// AppPassword is one of a user's app passwords, without the password itself (shown once, when it is created).
type AppPassword struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedBy string     `json:"lastUsedBy,omitempty"`
}

// Store keeps the two-step verification of every user. Its tables are db.SonusSchema: created by db.Init on the
// server, and here for a database that did not go through it (tests).
type Store struct {
	db      *sql.DB
	now     func() time.Time
	once    sync.Once
	initErr error
}

// New is a store on a database (tests); Instance is the server's.
func New(d *sql.DB) *Store { return &Store{db: d, now: time.Now} }

var defaultStore = sync.OnceValue(func() *Store { return New(db.Db()) })

// Instance is the store on the server's database.
func Instance() *Store { return defaultStore() }

func (s *Store) ready() error {
	s.once.Do(func() {
		ctx := context.Background()
		if _, s.initErr = s.db.ExecContext(ctx, db.SonusSchema); s.initErr != nil {
			return
		}
		if s.initErr = db.SonusUpgrade(ctx, s.db); s.initErr != nil {
			return
		}
		s.initErr = s.fillHashes(ctx)
	})
	return s.initErr
}

// fillHashes gives the app passwords made before API keys their hash, so they work as API keys too.
func (s *Store) fillHashes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, secret FROM sonus_app_password WHERE secret_hash = ''`)
	if err != nil {
		return err
	}
	type row struct{ id, sealed string }
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.sealed); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()
	for _, r := range todo {
		password, err := open(ctx, r.sealed)
		if err != nil {
			continue // unreadable: it cannot match anything anyway
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE sonus_app_password SET secret_hash = ? WHERE id = ?`, keyHash(password), r.id); err != nil {
			return err
		}
	}
	return nil
}

// keyHash finds an app password from what a client sends as an API key: as shown or without its dashes, any case.
func keyHash(key string) string {
	normal := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(key)))
	sum := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(sum[:])
}

// Enabled: the user has two-step verification set up and the policy is not Off, so the account password alone opens
// nothing.
func (s *Store) Enabled(ctx context.Context, userID string) (bool, error) {
	if CurrentMode() == Off {
		return false, nil
	}
	if err := s.ready(); err != nil {
		return false, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sonus_two_factor WHERE user_id = ? AND confirmed_at IS NOT NULL`, userID).Scan(&n)
	return n > 0, err
}

// Status of a user's two-step verification.
func (s *Store) Status(ctx context.Context, userID string) (Status, error) {
	st := Status{Mode: CurrentMode()}
	if err := s.ready(); err != nil {
		return st, err
	}
	var confirmed sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT confirmed_at FROM sonus_two_factor WHERE user_id = ?`, userID).Scan(&confirmed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return st, err
	}
	if confirmed.Valid {
		st.Enabled = true
		st.EnabledAt = &confirmed.Time
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM sonus_recovery_code WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&st.RecoveryCodesLeft)
	return st, err
}

// BeginSetup makes a new authenticator secret for the user, kept aside until a code from it is confirmed (one already
// set up keeps working until then). It returns the secret and its otpauth:// link.
func (s *Store) BeginSetup(ctx context.Context, userID, account string) (secret, uri string, err error) {
	if err = s.ready(); err != nil {
		return "", "", err
	}
	if secret, err = NewSecret(); err != nil {
		return "", "", err
	}
	sealed, err := seal(ctx, secret)
	if err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sonus_two_factor (user_id, pending_secret) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET pending_secret = excluded.pending_secret`, userID, sealed)
	if err != nil {
		return "", "", err
	}
	return secret, URI(Issuer, account, secret), nil
}

// ConfirmSetup checks a code from the secret being set up; when it matches, that secret becomes the one in use and the
// user gets new recovery codes (the old ones stop working).
func (s *Store) ConfirmSetup(ctx context.Context, userID, code string) ([]string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var pending string
	err := s.db.QueryRowContext(ctx, `SELECT pending_secret FROM sonus_two_factor WHERE user_id = ?`, userID).Scan(&pending)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && pending == "") {
		return nil, ErrNoSetup
	}
	if err != nil {
		return nil, err
	}
	secret, err := open(ctx, pending)
	if err != nil {
		return nil, err
	}
	step := matchStep(secret, code, s.now())
	if step < 0 {
		return nil, ErrInvalidCode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE sonus_two_factor SET secret = pending_secret, pending_secret = '', confirmed_at = ?, last_step = ? WHERE user_id = ?`,
		s.now().UTC(), step, userID); err != nil {
		return nil, err
	}
	codes, err := replaceRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// Verify checks a code at sign-in: from the authenticator (each one once: a code seen before is refused), or one of the
// recovery codes (used up by it).
func (s *Store) Verify(ctx context.Context, userID, code string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if isRecoveryCode(code) {
		res, err := s.db.ExecContext(ctx, `UPDATE sonus_recovery_code SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
			s.now().UTC(), userID, hashRecoveryCode(code))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrInvalidCode
		}
		return nil
	}
	var sealed string
	err := s.db.QueryRowContext(ctx, `SELECT secret FROM sonus_two_factor WHERE user_id = ? AND confirmed_at IS NOT NULL`, userID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidCode
	}
	if err != nil {
		return err
	}
	secret, err := open(ctx, sealed)
	if err != nil {
		return err
	}
	step := matchStep(secret, code, s.now())
	if step < 0 {
		return ErrInvalidCode
	}
	// one use per code: someone who saw it go by cannot sign in with it too
	res, err := s.db.ExecContext(ctx, `UPDATE sonus_two_factor SET last_step = ? WHERE user_id = ? AND last_step < ?`, step, userID, step)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrInvalidCode
	}
	return nil
}

// NewRecoveryCodes replaces the user's recovery codes (after the caller checked a code).
func (s *Store) NewRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	codes, err := replaceRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// Disable removes the user's two-step verification and recovery codes (their app passwords stay). With the policy
// Required they are asked to set it up again at the next sign-in: an admin's reset for a lost phone.
func (s *Store) Disable(ctx context.Context, userID string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sonus_two_factor WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sonus_recovery_code WHERE user_id = ?`, userID)
	return err
}

// ---------------------------------------------------------------- app passwords

// CreateAppPassword makes an app password for one client or device ("书房电脑"), short enough to type in; the password
// itself is returned this once and never shown again. It is also an API key (OpenSubsonic apiKeyAuthentication).
func (s *Store) CreateAppPassword(ctx context.Context, userID, name string) (AppPassword, string, error) {
	return s.create(ctx, userID, name, 16)
}

// CreateAPIKey is an app password a client was handed by the browser sign-in (DeviceLogin): never typed, so longer.
func (s *Store) CreateAPIKey(ctx context.Context, userID, name string) (AppPassword, string, error) {
	return s.create(ctx, userID, name, 32)
}

func (s *Store) create(ctx context.Context, userID, name string, length int) (AppPassword, string, error) {
	if err := s.ready(); err != nil {
		return AppPassword{}, "", err
	}
	password, err := randomString(length, 4)
	if err != nil {
		return AppPassword{}, "", err
	}
	sealed, err := seal(ctx, password)
	if err != nil {
		return AppPassword{}, "", err
	}
	ap := AppPassword{ID: id.NewRandom(), Name: strings.TrimSpace(name), CreatedAt: s.now().UTC()}
	if ap.Name == "" {
		ap.Name = "App"
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sonus_app_password (id, user_id, name, secret, secret_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		ap.ID, userID, ap.Name, sealed, keyHash(password), ap.CreatedAt)
	return ap, password, err
}

// UserForAPIKey is the user an API key (an app password) belongs to, or "" for none; the use is noted as
// MatchAppPassword notes it.
func (s *Store) UserForAPIKey(ctx context.Context, key, client string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	var id, userID, by string
	var used sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, last_used_at, last_used_by FROM sonus_app_password WHERE secret_hash = ?`, keyHash(key)).
		Scan(&id, &userID, &used, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s.noteUse(ctx, id, used, by, client)
	return userID, nil
}

// RevokeAPIKey deletes the app password that is this key: a client signing out gives its own key back.
func (s *Store) RevokeAPIKey(ctx context.Context, key string) (bool, error) {
	if err := s.ready(); err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sonus_app_password WHERE secret_hash = ?`, keyHash(key))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// noteUse records who used an app password, at most every few minutes: clients sign every request.
func (s *Store) noteUse(ctx context.Context, id string, used sql.NullTime, by, client string) {
	now := s.now().UTC()
	if !used.Valid || now.Sub(used.Time) > 5*time.Minute || by != client {
		_, _ = s.db.ExecContext(ctx, `UPDATE sonus_app_password SET last_used_at = ?, last_used_by = ? WHERE id = ?`, now, client, id)
	}
}

// AppPasswords of a user, newest first.
func (s *Store) AppPasswords(ctx context.Context, userID string) ([]AppPassword, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at, last_used_by FROM sonus_app_password WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []AppPassword{}
	for rows.Next() {
		var ap AppPassword
		var used sql.NullTime
		if err := rows.Scan(&ap.ID, &ap.Name, &ap.CreatedAt, &used, &ap.LastUsedBy); err != nil {
			return nil, err
		}
		if used.Valid {
			ap.LastUsedAt = &used.Time
		}
		list = append(list, ap)
	}
	return list, rows.Err()
}

// DeleteAppPassword revokes one of the user's app passwords: false when it was not theirs or not there.
func (s *Store) DeleteAppPassword(ctx context.Context, userID, appPasswordID string) (bool, error) {
	if err := s.ready(); err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sonus_app_password WHERE id = ? AND user_id = ?`, appPasswordID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// MatchAppPassword: one of the user's app passwords passes check (which is asked about it as shown, with its dashes,
// and without them, the ways people type it). The match is noted with the client's name, at most every few minutes:
// clients sign every request.
func (s *Store) MatchAppPassword(ctx context.Context, userID string, check func(password string) bool, client string) (bool, error) {
	if err := s.ready(); err != nil {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, secret, last_used_at, last_used_by FROM sonus_app_password WHERE user_id = ?`, userID)
	if err != nil {
		return false, err
	}
	type candidate struct {
		id, sealed, by string
		used           sql.NullTime
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.sealed, &c.used, &c.by); err != nil {
			rows.Close()
			return false, err
		}
		all = append(all, c)
	}
	rows.Close()
	for _, c := range all {
		password, err := open(ctx, c.sealed)
		if err != nil {
			continue
		}
		if !check(password) && !check(strings.ReplaceAll(password, "-", "")) {
			continue
		}
		s.noteUse(ctx, c.id, c.used, c.by, client)
		return true, nil
	}
	return false, nil
}

// ---------------------------------------------------------------- recovery codes

const recoveryCodes = 10

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID string) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sonus_recovery_code WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, recoveryCodes)
	for len(codes) < recoveryCodes {
		code, err := randomString(10, 5)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO sonus_recovery_code (user_id, code_hash) VALUES (?, ?)`, userID, hashRecoveryCode(code)); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// isRecoveryCode: recovery codes have letters, authenticator codes only digits.
func isRecoveryCode(code string) bool {
	return strings.IndexFunc(normalizeCode(code), func(r rune) bool { return r < '0' || r > '9' }) >= 0
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(normalizeCode(code))))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- helpers

// alphabet for what people read and type: no 0/o, 1/l/i
const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// randomString is n random characters in groups ("abcd-efgh-…"), each equally likely.
func randomString(n, group int) (string, error) {
	limit := byte(256 - 256%len(alphabet)) // bytes past this would favour the first letters
	var sb strings.Builder
	buf := make([]byte, 32)
	for count := 0; count < n; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, v := range buf {
			if v >= limit || count == n {
				continue
			}
			if count > 0 && count%group == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(v)%len(alphabet)])
			count++
		}
	}
	return sb.String(), nil
}

// the keys secrets are sealed with: PasswordEncryptionKey when set, as Navidrome's passwords are; opened with the
// default key too, for what was sealed before one was set
func keys() [][]byte {
	def := sha256.Sum256([]byte(consts.DefaultEncryptionKey))
	if conf.Server.PasswordEncryptionKey == "" {
		return [][]byte{def[:]}
	}
	custom := sha256.Sum256([]byte(conf.Server.PasswordEncryptionKey))
	return [][]byte{custom[:], def[:]}
}

func seal(ctx context.Context, plain string) (string, error) {
	return utils.Encrypt(ctx, keys()[0], plain)
}

func open(ctx context.Context, sealed string) (string, error) {
	for _, k := range keys() {
		if plain, err := utils.Decrypt(ctx, k, sealed); err == nil {
			return plain, nil
		}
	}
	return "", ErrUnreadable
}
