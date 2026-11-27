package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Sentinel errors returned by Service. HTTP handlers map these to the
// frozen error envelope codes; they never expose the underlying store or
// SQLite errors.
var (
	// ErrAlreadyInitialized is returned by Setup when first-run setup has
	// already completed. Setup is permanently disabled afterwards.
	ErrAlreadyInitialized = errors.New("auth: already initialized")

	// ErrInvalidCredentials is returned by Login for every failed login —
	// unknown email, wrong password, or an inactive user — so callers
	// cannot distinguish them (no account enumeration).
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrEmailTaken is returned by CreateUser when another user already
	// has the requested email.
	ErrEmailTaken = errors.New("auth: email already in use")

	// ErrSlugTaken is returned when an author slug already belongs to
	// another user.
	ErrSlugTaken = errors.New("auth: author slug already in use")

	// ErrLastOwner is returned by UpdateUser when a change would deactivate
	// or demote the last remaining active owner.
	ErrLastOwner = errors.New("auth: cannot remove the last active owner")
)

// dummyPasswordHash is verified against on the unknown-email login path so
// that a login attempt does the same argon2 work whether or not the email
// exists, keeping response timing from revealing account existence.
var dummyPasswordHash = mustHash("pocketpress-timing-equalizer-not-a-real-account")

func mustHash(password string) string {
	h, err := HashPassword(password)
	if err != nil {
		panic("auth: failed to build dummy password hash: " + err.Error())
	}
	return h
}

// Service holds the dependencies of the auth domain: the SQLite handle and
// a logger. It is safe for concurrent use (it holds no per-request state).
type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewService builds a Service over the given database handle. logger may be
// nil, in which case a no-op logger is used.
func NewService(db *sql.DB, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, logger: logger}
}

// IsInitialized reports whether first-run setup has completed.
func (s *Service) IsInitialized(ctx context.Context) (bool, error) {
	return store.IsInitialized(ctx, s.db)
}

// SetupInput carries the first-run setup fields. Validation (required
// fields, email/URL/timezone formats, password length) happens in the HTTP
// layer before Setup is called; Setup enforces only the invariant that
// setup runs at most once.
type SetupInput struct {
	SiteName   string
	SiteURL    string
	OwnerName  string
	OwnerEmail string
	Password   string
	Timezone   string
}

// Setup performs first-run setup: it creates the owner account and the
// singleton settings row, then opens a session for the owner and returns
// the raw session token alongside the created user.
//
// If setup has already completed it returns ErrAlreadyInitialized without
// modifying anything.
func (s *Service) Setup(ctx context.Context, in SetupInput) (store.User, string, error) {
	initialized, err := store.IsInitialized(ctx, s.db)
	if err != nil {
		return store.User{}, "", err
	}
	if initialized {
		return store.User{}, "", ErrAlreadyInitialized
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return store.User{}, "", err
	}

	slug, err := s.uniqueAuthorSlug(ctx, in.OwnerName, "")
	if err != nil {
		return store.User{}, "", err
	}

	owner, err := store.CreateUser(ctx, s.db, store.NewUser{
		Email:        in.OwnerEmail,
		PasswordHash: hash,
		Name:         in.OwnerName,
		Role:         store.RoleOwner,
		Slug:         slug,
	})
	if err != nil {
		return store.User{}, "", err
	}

	if _, err := store.CreateSettings(ctx, s.db, store.Settings{
		SiteName:         in.SiteName,
		SiteURL:          in.SiteURL,
		AdminTitle:       "PocketPress",
		Locale:           "en",
		AvailableLocales: []string{"en"},
		Timezone:         in.Timezone,
		CORSOrigins:      []string{},
		PublicAPIEnabled: true,
		WebhookUserAgent: "PocketPress",
	}); err != nil {
		return store.User{}, "", err
	}

	token, err := s.createSession(ctx, owner.ID)
	if err != nil {
		return store.User{}, "", err
	}

	s.logger.Info("first-run setup completed", slog.String("owner_id", owner.ID), slog.String("email", owner.Email))
	return owner, token, nil
}

// Login verifies an email/password pair and, on success, records the login
// time, purges expired sessions, and opens a new session — returning the
// user and the raw session token.
//
// Every failure mode (unknown email, wrong password, inactive user) returns
// ErrInvalidCredentials, and the argon2 comparison runs even when the email
// is unknown, so neither the response nor its timing reveals which accounts
// exist.
func (s *Service) Login(ctx context.Context, email, password string) (store.User, string, error) {
	user, err := store.GetUserByEmail(ctx, s.db, email)
	if errors.Is(err, store.ErrNotFound) {
		// Do equivalent work to a real verify so timing does not leak that
		// the account is missing.
		_, _ = VerifyPassword(password, dummyPasswordHash)
		s.logger.Warn("failed admin login", slog.String("email", email), slog.String("reason", "unknown_email"))
		return store.User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return store.User{}, "", err
	}

	ok, err := VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return store.User{}, "", err
	}
	if !ok {
		s.logger.Warn("failed admin login", slog.String("email", email), slog.String("reason", "bad_password"))
		return store.User{}, "", ErrInvalidCredentials
	}
	if !user.Active {
		s.logger.Warn("failed admin login", slog.String("email", email), slog.String("reason", "inactive"))
		return store.User{}, "", ErrInvalidCredentials
	}

	now := time.Now().UTC()
	if err := store.UpdateUserLastLogin(ctx, s.db, user.ID, now); err != nil {
		return store.User{}, "", err
	}
	if _, err := store.DeleteExpiredSessions(ctx, s.db, now); err != nil {
		return store.User{}, "", err
	}

	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return store.User{}, "", err
	}

	user.LastLoginAt = &now
	s.logger.Info("admin login", slog.String("user_id", user.ID), slog.String("email", user.Email))
	return user, token, nil
}

// Logout deletes the session backing token, if any. It is idempotent: an
// empty or unknown token is a no-op that returns nil, so logging out
// without a valid session still succeeds.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := store.DeleteSessionByTokenHash(ctx, s.db, hashToken(token)); err != nil {
		return err
	}
	s.logger.Info("admin logout")
	return nil
}

// UserForSessionToken resolves a raw session token to its user, returning
// store.ErrNotFound if the token has no matching session, the session has
// expired, or the owning user is inactive. It is the resolver the
// RequireUser middleware calls on each request.
func (s *Service) UserForSessionToken(ctx context.Context, token string) (store.User, error) {
	if token == "" {
		return store.User{}, store.ErrNotFound
	}
	sess, err := store.GetValidSessionByTokenHash(ctx, s.db, hashToken(token), time.Now().UTC())
	if err != nil {
		return store.User{}, err
	}
	return store.GetUserByID(ctx, s.db, sess.UserID)
}

// ListUsers returns every user, oldest first.
func (s *Service) ListUsers(ctx context.Context) ([]store.User, error) {
	return store.ListUsers(ctx, s.db)
}

// NewUserInput carries the fields for owner-driven user creation. As with
// SetupInput, field validation happens in the HTTP layer.
type NewUserInput struct {
	Name     string
	Email    string
	Password string
	Role     string
}

// CreateUser creates a new active user. It returns ErrEmailTaken if the
// email already belongs to another user.
func (s *Service) CreateUser(ctx context.Context, in NewUserInput) (store.User, error) {
	if _, err := store.GetUserByEmail(ctx, s.db, in.Email); err == nil {
		return store.User{}, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return store.User{}, err
	}

	slug, err := s.uniqueAuthorSlug(ctx, in.Name, "")
	if err != nil {
		return store.User{}, err
	}

	user, err := store.CreateUser(ctx, s.db, store.NewUser{
		Email:        in.Email,
		PasswordHash: hash,
		Name:         in.Name,
		Role:         in.Role,
		Slug:         slug,
	})
	if err != nil {
		return store.User{}, err
	}

	s.logger.Info("admin user created", slog.String("user_id", user.ID), slog.String("role", user.Role))
	return user, nil
}

// UpdateUserInput is a partial update: each non-nil field is applied, each
// nil field is left unchanged. It mirrors the PATCH body's optional
// name/role/active/password/profile fields.
type UpdateUserInput struct {
	Name     *string
	Slug     *string
	Bio      *string
	Website  *string
	Location *string
	Avatar   *string // media id; pointer to "" clears
	Role     *string
	Active   *bool
	Password *string
}

// UpdateUser applies a partial update to the user with the given id.
//
// It enforces the last-active-owner guard: if the target is currently the
// only active owner, a change that would deactivate it or demote it from
// owner returns ErrLastOwner and nothing is modified. It returns
// store.ErrNotFound if no such user exists, or ErrSlugTaken on a slug
// collision.
func (s *Service) UpdateUser(ctx context.Context, id string, in UpdateUserInput) (store.User, error) {
	user, err := store.GetUserByID(ctx, s.db, id)
	if err != nil {
		return store.User{}, err
	}

	demoting := in.Role != nil && *in.Role != store.RoleOwner && user.Role == store.RoleOwner
	deactivating := in.Active != nil && !*in.Active && user.Active
	if demoting || deactivating {
		if user.Role == store.RoleOwner && user.Active {
			activeOwners, err := s.activeOwnerCount(ctx)
			if err != nil {
				return store.User{}, err
			}
			if activeOwners <= 1 {
				return store.User{}, ErrLastOwner
			}
		}
	}

	if in.Name != nil {
		if err := store.SetUserName(ctx, s.db, id, *in.Name); err != nil {
			return store.User{}, err
		}
	}
	if in.Slug != nil {
		slug := content.Slugify(*in.Slug)
		if slug == "" {
			return store.User{}, fmt.Errorf("auth: author slug is required")
		}
		taken, err := store.UserSlugExists(ctx, s.db, slug, id)
		if err != nil {
			return store.User{}, err
		}
		if taken {
			return store.User{}, ErrSlugTaken
		}
		if err := store.SetUserSlug(ctx, s.db, id, slug); err != nil {
			return store.User{}, err
		}
	}
	if in.Bio != nil || in.Website != nil || in.Location != nil || in.Avatar != nil {
		bio, website, location, avatar := user.Bio, user.Website, user.Location, user.Avatar
		if in.Bio != nil {
			bio = strings.TrimSpace(*in.Bio)
		}
		if in.Website != nil {
			website = strings.TrimSpace(*in.Website)
		}
		if in.Location != nil {
			location = strings.TrimSpace(*in.Location)
		}
		if in.Avatar != nil {
			avatar = strings.TrimSpace(*in.Avatar)
			if avatar != "" {
				if _, err := store.GetMediaByID(ctx, s.db, avatar); err != nil {
					return store.User{}, err
				}
			}
		}
		if err := store.SetUserProfile(ctx, s.db, id, bio, website, location, avatar); err != nil {
			return store.User{}, err
		}
	}
	if in.Role != nil {
		if err := store.SetUserRole(ctx, s.db, id, *in.Role); err != nil {
			return store.User{}, err
		}
	}
	if in.Active != nil {
		if err := store.SetUserActive(ctx, s.db, id, *in.Active); err != nil {
			return store.User{}, err
		}
	}
	if in.Password != nil {
		hash, err := HashPassword(*in.Password)
		if err != nil {
			return store.User{}, err
		}
		if err := store.SetUserPasswordHash(ctx, s.db, id, hash); err != nil {
			return store.User{}, err
		}
	}

	updated, err := store.GetUserByID(ctx, s.db, id)
	if err != nil {
		return store.User{}, err
	}
	s.logger.Info("admin user updated", slog.String("user_id", id))
	return updated, nil
}

// uniqueAuthorSlug returns a free author slug derived from name. When name
// yields an empty slug, excludeID (or a fixed "author" base) seeds the
// candidate. Collisions append "-2", "-3", …
func (s *Service) uniqueAuthorSlug(ctx context.Context, name, excludeID string) (string, error) {
	base := content.Slugify(name)
	if base == "" {
		base = "author"
	}
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			suffix := "-" + strconv.Itoa(i)
			maxBase := content.MaxSlugLen - len(suffix)
			if maxBase < 0 {
				maxBase = 0
			}
			candidate = strings.TrimRight(base, "-")
			if len(candidate) > maxBase {
				candidate = strings.TrimRight(candidate[:maxBase], "-")
			}
			candidate += suffix
		}
		taken, err := store.UserSlugExists(ctx, s.db, candidate, excludeID)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
}

// activeOwnerCount counts users that are both owners and active. The store
// exposes a role count that ignores active status, so the active filter is
// applied here over the (small) user list.
func (s *Service) activeOwnerCount(ctx context.Context) (int, error) {
	users, err := store.ListUsers(ctx, s.db)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u.Role == store.RoleOwner && u.Active {
			n++
		}
	}
	return n, nil
}

// createSession generates a token, stores its hash with a 30-day expiry,
// and returns the raw token for the caller to put in the session cookie.
func (s *Service) createSession(ctx context.Context, userID string) (string, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", err
	}
	if _, err := store.CreateSession(ctx, s.db, store.NewSession{
		UserID:    userID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().UTC().Add(SessionTTL),
	}); err != nil {
		return "", err
	}
	return token, nil
}
