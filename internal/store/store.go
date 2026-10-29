// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package store keeps the sysop password, settings, sessions, and
// articles in a SQLite file under the state directory.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	bcryptCost       = 12
	minPasswordRunes = 12
	maxPasswordBytes = 72
	sessionHours     = 12
	schemaVersion    = 1
)

// ErrNoPassword means serve was started before a sysop password was set.
var ErrNoPassword = errors.New("no sysop password is set")

// ErrNoSession means the cookie does not match a live session.
var ErrNoSession = errors.New("session not found")

// ErrNotFound means the article row does not exist.
var ErrNotFound = errors.New("article not found")

// ErrNoState means the state directory is not there.
var ErrNoState = errors.New("state directory does not exist")

// ErrNoDatabase means the directory has no Crossbeam database.
var ErrNoDatabase = errors.New("state directory has no database")

// ErrDuplicateID means another row already uses that bulletin id.
type ErrDuplicateID struct{}

func (ErrDuplicateID) Error() string { return "id is already used" }

// Store is one state directory.
type Store struct {
	db   *sql.DB
	dir  string
	path string

	// bulletinMu covers the in-flight bulletin claims in pendN and pendB.
	// A forward decides to accept before the body arrives, so the claim
	// is counted until the row is stored or the body is rejected.
	bulletinMu sync.Mutex
	pendN      int
	pendB      int64
	pendID     map[string]int
}

// Open creates the state directory and the database. The directory is
// mode 0700 and the database file is mode 0600.
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	dir = abs
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "crossbeam.db")
	dsnText, err := dsn(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsnText)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{db: db, dir: dir, path: path}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	s.tighten()
	return s, nil
}

// OpenExisting opens a state directory that already has a database.
// It does not create the directory or the database file.
func OpenExisting(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoState
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNoState
	}
	dbPath := filepath.Join(abs, "crossbeam.db")
	info, err = os.Stat(dbPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoDatabase
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrNoDatabase
	}
	return Open(abs)
}

// Close closes the database.
func (s *Store) Close() error {
	s.tighten()
	return s.db.Close()
}

// Dir is the state directory.
func (s *Store) Dir() string { return s.dir }

func dsn(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// A relative path becomes a file URL whose first directory is the host.
	// The driver then opens the wrong file. An absolute path stays a path.
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	// RawQuery is used as written. Encoding the parentheses would hide
	// the pragma from the driver.
	u := url.URL{
		Scheme:   "file",
		Path:     slash,
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate",
	}
	return u.String(), nil
}

func (s *Store) tighten() {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := s.path + suffix
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

func (s *Store) migrate(ctx context.Context) error {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_meta'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return s.create(ctx)
	}
	if err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_meta LIMIT 1`).Scan(&version); err != nil {
		return err
	}
	if version != schemaVersion {
		return fmt.Errorf("state database version %d is not supported", version)
	}
	return nil
}

func (s *Store) create(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE schema_meta (
  version INTEGER NOT NULL
);
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE sysop (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  password_hash TEXT NOT NULL
);
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  csrf TEXT NOT NULL,
  expires INTEGER NOT NULL
);
CREATE TABLE articles (
  row_id INTEGER PRIMARY KEY,
  bulletin_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  author TEXT NOT NULL,
  created TEXT NOT NULL,
  body TEXT NOT NULL
);
CREATE UNIQUE INDEX articles_bulletin_id ON articles (bulletin_id) WHERE bulletin_id != '';
INSERT INTO schema_meta (version) VALUES (1);
`)
	return err
}

// HasPassword reports whether a sysop password is stored.
func (s *Store) HasPassword(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sysop WHERE id = 1 AND password_hash != ''`).Scan(&n)
	return n == 1, err
}

// SetPassword replaces the sysop password and deletes every session.
// The password is never written except as a bcrypt hash.
func (s *Store) SetPassword(ctx context.Context, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sysop (id, password_hash) VALUES (1, ?)
ON CONFLICT (id) DO UPDATE SET password_hash = excluded.password_hash
`, string(hash)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.tighten()
	return nil
}

// ValidatePassword checks length and control characters. Spaces are kept.
// Callers trim trailing CR and LF from the input line before this.
func ValidatePassword(password string) error {
	for _, r := range password {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("password contains a control character")
		}
	}
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return fmt.Errorf("password must be at least 12 characters")
	}
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("password is longer than 72 bytes")
	}
	return nil
}

const unsetPasswordMaterial = "crossbeam-unset-password"

// invalidGuess is the bcrypt input for a password that fails validation.
// The leading NUL means ValidatePassword rejects it, so SetPassword can
// never store this value.
const invalidGuess = "\x00invalid-password-pad"

var (
	dummyOnce sync.Once
	dummyHash []byte
	dummyErr  error
)

func dummyBcrypt() ([]byte, error) {
	dummyOnce.Do(func() {
		dummyHash, dummyErr = bcrypt.GenerateFromPassword([]byte(unsetPasswordMaterial), bcryptCost)
	})
	return dummyHash, dummyErr
}

// CheckPassword reports whether password matches the stored hash.
// A missing hash is still checked with bcrypt so the two cases take
// the same amount of time. The answer is false when no password is set.
// A guess that fails validation is compared as invalidGuess, which
// itself fails validation, so it cannot be the stored password.
func (s *Store) CheckPassword(ctx context.Context, password string) (bool, error) {
	hash, err := s.passwordHash(ctx)
	if err != nil {
		return false, err
	}
	return comparePassword(hash, password)
}

func (s *Store) passwordHash(ctx context.Context) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM sysop WHERE id = 1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

func comparePassword(hash, password string) (bool, error) {
	compare := password
	if ValidatePassword(password) != nil {
		compare = invalidGuess
	}
	if hash == "" {
		dummy, err := dummyBcrypt()
		if err != nil {
			return false, err
		}
		_ = bcrypt.CompareHashAndPassword(dummy, []byte(compare))
		return false, nil
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(compare)) == nil, nil
}

// testHookBeforeLoginTx runs after a password matches and before the
// session row is written. Tests use it to change the password in that window.
var testHookBeforeLoginTx func()

// Login checks the password and stores a new session. The hash is read
// again inside the write transaction, so a password change in between
// does not leave a session for the old password. No session is created
// when the password is unset.
func (s *Store) Login(ctx context.Context, password string) (Session, bool, error) {
	hash, err := s.passwordHash(ctx)
	if err != nil {
		return Session{}, false, err
	}
	ok, err := comparePassword(hash, password)
	if err != nil || !ok {
		return Session{}, false, err
	}
	if testHookBeforeLoginTx != nil {
		testHookBeforeLoginTx()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, false, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT password_hash FROM sysop WHERE id = 1`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current != hash) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	session, err := insertSession(ctx, tx)
	if err != nil {
		return Session{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, false, err
	}
	s.tighten()
	return session, true, nil
}

// Settings returns every schema value, including defaults for keys that
// have not been saved. The map is read from SQLite on each call.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	return s.settingsTx(ctx, nil)
}

func (s *Store) settingsTx(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	q := `SELECT key, value FROM settings`
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = tx.QueryContext(ctx, q)
	} else {
		rows, err = s.db.QueryContext(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := settings.Defaults()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if _, ok := settings.ByKey(key); !ok {
			return nil, fmt.Errorf("unknown setting in the state database")
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return settings.Validate(out)
}

// Apply merges patch onto the saved settings and stores the result when
// it is valid. csrf is ignored. An invalid patch leaves the database
// unchanged.
func (s *Store) Apply(ctx context.Context, patch map[string]string) (map[string]string, error) {
	if patch == nil {
		patch = map[string]string{}
	}
	delete(patch, "csrf")
	for key := range patch {
		if _, ok := settings.ByKey(key); !ok {
			return nil, unknownSetting(key)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := s.settingsTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	for key, value := range patch {
		current[key] = value
	}
	next, err := settings.Validate(current)
	if err != nil {
		return nil, err
	}
	for key, value := range next {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value
`, key, value); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.tighten()
	return next, nil
}

func unknownSetting(key string) error {
	if len(key) > 64 || !utf8.ValidString(key) {
		return fmt.Errorf("unknown setting")
	}
	return fmt.Errorf("unknown setting %q", key)
}

// Session is a new login.
type Session struct {
	Token   string
	CSRF    string
	Expires time.Time
}

// CreateSession stores only the SHA-256 of the token. Login is the
// path that checks a password first.
func (s *Store) CreateSession(ctx context.Context) (Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	session, err := insertSession(ctx, tx)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	s.tighten()
	return session, nil
}

func insertSession(ctx context.Context, tx *sql.Tx) (Session, error) {
	raw := make([]byte, 32)
	csrfRaw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Session{}, err
	}
	if _, err := rand.Read(csrfRaw); err != nil {
		return Session{}, err
	}
	token := hex.EncodeToString(raw)
	csrf := hex.EncodeToString(csrfRaw)
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(sessionHours * time.Hour)
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires <= ?`, time.Now().Unix()); err != nil {
		return Session{}, err
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO sessions (token_hash, csrf, expires) VALUES (?, ?, ?)
`, hex.EncodeToString(sum[:]), csrf, expires.Unix())
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, CSRF: csrf, Expires: expires}, nil
}

// LookupSession returns the CSRF token for a live session cookie.
func (s *Store) LookupSession(ctx context.Context, token string) (string, error) {
	if len(token) != 64 {
		return "", ErrNoSession
	}
	sum := sha256.Sum256([]byte(token))
	var csrf string
	var expires int64
	err := s.db.QueryRowContext(ctx, `
SELECT csrf, expires FROM sessions WHERE token_hash = ?
`, hex.EncodeToString(sum[:])).Scan(&csrf, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", err
	}
	if time.Now().Unix() >= expires {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hex.EncodeToString(sum[:]))
		return "", ErrNoSession
	}
	return csrf, nil
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	sum := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hex.EncodeToString(sum[:]))
	return err
}

// ArticleIn is a compose form before it is checked.
type ArticleIn struct {
	RowID   int64
	ID      string
	Title   string
	Author  string
	Created string
	Body    string
	Now     time.Time
}

// SavedArticle is one row.
type SavedArticle struct {
	RowID   int64
	Article article.Article
}

// Prepare fills an empty author from station.callsign and an empty
// created time from now, then returns the canonical article. It does
// not write.
func (s *Store) Prepare(ctx context.Context, in ArticleIn) (article.Article, error) {
	current, err := s.Settings(ctx)
	if err != nil {
		return article.Article{}, err
	}
	author := in.Author
	if strings.TrimSpace(author) == "" {
		author = current[settings.KeyCallsign]
	}
	created, err := ParseCreated(in.Created, in.Now)
	if err != nil {
		return article.Article{}, err
	}
	return (article.Article{
		ID:      in.ID,
		Title:   in.Title,
		Author:  author,
		Created: created,
		Body:    in.Body,
	}).Canonical()
}

// ParseCreated reads RFC3339. An empty value uses now.
func ParseCreated(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if now.IsZero() {
			now = time.Now()
		}
		return now, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("created time is not RFC3339")
	}
	return parsed, nil
}

// SaveArticle inserts a row, or updates one when RowID is set.
func (s *Store) SaveArticle(ctx context.Context, in ArticleIn) (SavedArticle, error) {
	canonical, err := s.Prepare(ctx, in)
	if err != nil {
		return SavedArticle{}, err
	}
	created := canonical.Created.Format(time.RFC3339)
	if in.RowID == 0 {
		res, err := s.db.ExecContext(ctx, `
INSERT INTO articles (bulletin_id, title, author, created, body)
VALUES (?, ?, ?, ?, ?)
`, canonical.ID, canonical.Title, canonical.Author, created, canonical.Body)
		if err != nil {
			if isUnique(err) {
				return SavedArticle{}, ErrDuplicateID{}
			}
			return SavedArticle{}, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return SavedArticle{}, err
		}
		s.tighten()
		return SavedArticle{RowID: id, Article: canonical}, nil
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE articles
SET bulletin_id = ?, title = ?, author = ?, created = ?, body = ?
WHERE row_id = ?
`, canonical.ID, canonical.Title, canonical.Author, created, canonical.Body, in.RowID)
	if err != nil {
		if isUnique(err) {
			return SavedArticle{}, ErrDuplicateID{}
		}
		return SavedArticle{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SavedArticle{}, err
	}
	if n == 0 {
		return SavedArticle{}, ErrNotFound
	}
	s.tighten()
	return SavedArticle{RowID: in.RowID, Article: canonical}, nil
}

// Article loads one row. A corrupt created time fails the read.
func (s *Store) Article(ctx context.Context, rowID int64) (SavedArticle, error) {
	var saved SavedArticle
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT row_id, bulletin_id, title, author, created, body
FROM articles WHERE row_id = ?
`, rowID).Scan(&saved.RowID, &saved.Article.ID, &saved.Article.Title, &saved.Article.Author, &created, &saved.Article.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedArticle{}, ErrNotFound
	}
	if err != nil {
		return SavedArticle{}, err
	}
	parsed, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return SavedArticle{}, fmt.Errorf("created time is not RFC3339")
	}
	saved.Article.Created = parsed
	if _, err := saved.Article.Canonical(); err != nil {
		return SavedArticle{}, err
	}
	return saved, nil
}

// ListArticles returns rows newest first, without bodies. Reopen one
// row with Article.
func (s *Store) ListArticles(ctx context.Context) ([]SavedArticle, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT row_id, bulletin_id, title, author, created
FROM articles
ORDER BY created DESC, row_id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedArticle
	for rows.Next() {
		var saved SavedArticle
		var created string
		if err := rows.Scan(&saved.RowID, &saved.Article.ID, &saved.Article.Title, &saved.Article.Author, &created); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, fmt.Errorf("created time is not RFC3339")
		}
		saved.Article.Created = parsed
		out = append(out, saved)
	}
	return out, rows.Err()
}

// HasBulletinID reports whether a non-empty bulletin id is already stored.
// The match is case-insensitive. Wire ids are uppercase.
func (s *Store) HasBulletinID(ctx context.Context, id string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE bulletin_id = ? COLLATE NOCASE`, id).Scan(&n)
	return n > 0, err
}

// ClaimBulletin reserves one inbound bulletin under the store cap.
// The answer is '+' when the slot is reserved, '-' when the id is
// already stored, and '=' when the cap is already reached. The caller
// releases the reservation when the body is stored or rejected.
func (s *Store) ClaimBulletin(ctx context.Context, id string, size int, maxN int, maxB int64) (byte, error) {
	s.bulletinMu.Lock()
	defer s.bulletinMu.Unlock()
	have, err := s.HasBulletinID(ctx, id)
	if err != nil {
		return 0, err
	}
	key := strings.ToUpper(strings.TrimSpace(id))
	if have || s.pendID[key] > 0 {
		return '-', nil
	}
	n, b, err := s.BulletinUsage(ctx)
	if err != nil {
		return 0, err
	}
	if n+s.pendN >= maxN || b+s.pendB+int64(size) > maxB {
		return '=', nil
	}
	if s.pendID == nil {
		s.pendID = map[string]int{}
	}
	s.pendID[key]++
	s.pendN++
	s.pendB += int64(size)
	return '+', nil
}

// ReleaseBulletin drops one reservation from ClaimBulletin.
func (s *Store) ReleaseBulletin(id string, size int) {
	s.bulletinMu.Lock()
	defer s.bulletinMu.Unlock()
	key := strings.ToUpper(strings.TrimSpace(id))
	if s.pendID[key] > 0 {
		s.pendID[key]--
		if s.pendID[key] == 0 {
			delete(s.pendID, key)
		}
	}
	if s.pendN > 0 {
		s.pendN--
	}
	s.pendB -= int64(size)
	if s.pendB < 0 {
		s.pendB = 0
	}
}

// AllArticles returns every row oldest first, including bodies.
func (s *Store) AllArticles(ctx context.Context) ([]SavedArticle, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT row_id, bulletin_id, title, author, created, body
FROM articles
ORDER BY row_id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedArticle
	for rows.Next() {
		var saved SavedArticle
		var created string
		if err := rows.Scan(&saved.RowID, &saved.Article.ID, &saved.Article.Title, &saved.Article.Author, &created, &saved.Article.Body); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, fmt.Errorf("created time is not RFC3339")
		}
		saved.Article.Created = parsed
		if _, err := saved.Article.Canonical(); err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, rows.Err()
}

// BulletinSource is one article without its body. BodyBytes is the
// stored body length in bytes.
type BulletinSource struct {
	RowID     int64
	ID        string
	Title     string
	Author    string
	Created   time.Time
	BodyBytes int
}

// BulletinSources returns every row oldest first, without bodies.
func (s *Store) BulletinSources(ctx context.Context) ([]BulletinSource, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT row_id, bulletin_id, title, author, created, LENGTH(CAST(body AS BLOB))
FROM articles
ORDER BY row_id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BulletinSource
	for rows.Next() {
		var src BulletinSource
		var created string
		if err := rows.Scan(&src.RowID, &src.ID, &src.Title, &src.Author, &created, &src.BodyBytes); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, fmt.Errorf("created time is not RFC3339")
		}
		src.Created = parsed
		out = append(out, src)
	}
	return out, rows.Err()
}

// BulletinUsage reports how many articles are stored and how many body
// bytes they hold.
func (s *Store) BulletinUsage(ctx context.Context) (articles int, bodyBytes int64, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(LENGTH(CAST(body AS BLOB))), 0) FROM articles
`).Scan(&articles, &bodyBytes)
	return articles, bodyBytes, err
}

// DeleteArticle removes one row. A missing row is ErrNotFound.
func (s *Store) DeleteArticle(ctx context.Context, rowID int64) error {
	if rowID <= 0 {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM articles WHERE row_id = ?`, rowID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	s.tighten()
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
