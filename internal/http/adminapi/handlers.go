package adminapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketpress/pocketpress/internal/auth"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// setupStatus (GET /setup/status, public) reports whether first-run setup
// has completed. The admin UI polls it to decide whether to show the setup
// screen or the login screen.
func (h *Handler) setupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := h.svc.IsInitialized(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"initialized": initialized})
}

// setup (POST /setup, public) performs first-run setup and signs the new
// owner in. Once PocketPress is initialized it responds 403
// "already_initialized".
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteName   string `json:"site_name"`
		SiteURL    string `json:"site_url"`
		OwnerName  string `json:"owner_name"`
		OwnerEmail string `json:"owner_email"`
		Password   string `json:"password"`
		Timezone   string `json:"timezone"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	fields := map[string]string{}
	if strings.TrimSpace(req.SiteName) == "" {
		fields["site_name"] = "Site name is required."
	}
	switch {
	case strings.TrimSpace(req.SiteURL) == "":
		fields["site_url"] = "Site URL is required."
	case !isValidHTTPURL(req.SiteURL):
		fields["site_url"] = "Site URL must be a valid http(s) URL."
	}
	if strings.TrimSpace(req.OwnerName) == "" {
		fields["owner_name"] = "Owner name is required."
	}
	switch {
	case strings.TrimSpace(req.OwnerEmail) == "":
		fields["owner_email"] = "Owner email is required."
	case !isValidEmail(req.OwnerEmail):
		fields["owner_email"] = "Owner email must be a valid email address."
	}
	switch {
	case req.Password == "":
		fields["password"] = "Password is required."
	case len(req.Password) < minPasswordLen:
		fields["password"] = "Password must be at least 10 characters."
	}
	switch {
	case strings.TrimSpace(req.Timezone) == "":
		fields["timezone"] = "Timezone is required."
	case !isValidTimezone(req.Timezone):
		fields["timezone"] = "Timezone must be a valid IANA timezone."
	}
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	user, token, err := h.svc.Setup(r.Context(), auth.SetupInput{
		SiteName:   req.SiteName,
		SiteURL:    req.SiteURL,
		OwnerName:  req.OwnerName,
		OwnerEmail: req.OwnerEmail,
		Password:   req.Password,
		Timezone:   req.Timezone,
	})
	if errors.Is(err, auth.ErrAlreadyInitialized) {
		middleware.WriteError(w, http.StatusForbidden, "already_initialized", "PocketPress has already been set up.")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}

	h.setSessionCookie(w, token)
	h.writeUser(w, http.StatusCreated, user)
}

// login (POST /auth/login, public) verifies credentials and opens a
// session. Every credential failure is an identical 401
// "invalid_credentials".
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	fields := map[string]string{}
	if strings.TrimSpace(req.Email) == "" {
		fields["email"] = "Email is required."
	}
	if req.Password == "" {
		fields["password"] = "Password is required."
	}
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	user, token, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}

	h.setSessionCookie(w, token)
	h.writeUser(w, http.StatusOK, user)
}

// logout (POST /auth/logout, public) deletes the session (if any) and
// clears the cookie. It always succeeds with 204, even without a valid
// session.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token := ""
	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		token = cookie.Value
	}
	if err := h.svc.Logout(r.Context(), token); err != nil {
		// Deleting the session row failed, but the client cookie can still
		// be cleared — log it and continue rather than failing logout.
		h.logger.Warn("logout session cleanup failed", "error", err.Error())
	}

	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// me (GET /auth/me, authenticated) returns the current user. RequireUser
// has already placed the user in the request context.
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	h.writeUser(w, http.StatusOK, user)
}

// updateMe (PATCH /auth/me, authenticated) lets any signed-in user edit
// their own profile (name, slug, bio, website, location, avatar, password).
// Role and active status are owner-only via PATCH /users/{id}.
func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}

	var req profilePatchBody
	if !h.decode(w, r, &req) {
		return
	}
	fields := validateProfilePatch(req)
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	updated, err := h.svc.UpdateUser(r.Context(), user.ID, auth.UpdateUserInput{
		Name:     req.Name,
		Slug:     req.Slug,
		Bio:      req.Bio,
		Website:  req.Website,
		Location: req.Location,
		Avatar:   req.Avatar,
		Password: req.Password,
	})
	switch {
	case errors.Is(err, auth.ErrSlugTaken):
		h.validationError(w, map[string]string{"slug": "This author slug is already in use."})
		return
	case errors.Is(err, store.ErrNotFound):
		// Avatar media id missing, or the session user vanished.
		if req.Avatar != nil && *req.Avatar != "" {
			h.validationError(w, map[string]string{"avatar": "Avatar media was not found."})
			return
		}
		middleware.WriteError(w, http.StatusNotFound, "not_found", "User not found.")
		return
	case err != nil:
		h.serverError(w, err)
		return
	}

	h.writeUser(w, http.StatusOK, updated)
}

// profilePatchBody is the shared PATCH shape for self-profile and (with
// role/active) owner user updates.
type profilePatchBody struct {
	Name     *string `json:"name"`
	Slug     *string `json:"slug"`
	Bio      *string `json:"bio"`
	Website  *string `json:"website"`
	Location *string `json:"location"`
	Avatar   *string `json:"avatar"`
	Password *string `json:"password"`
}

const (
	bioMaxLen      = 500
	locationMaxLen = 100
)

func validateProfilePatch(req profilePatchBody) map[string]string {
	fields := map[string]string{}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		fields["name"] = "Name cannot be empty."
	}
	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" {
			fields["slug"] = "Author slug cannot be empty."
		}
	}
	if req.Bio != nil && len([]rune(*req.Bio)) > bioMaxLen {
		fields["bio"] = "Bio must be at most 500 characters."
	}
	if req.Location != nil && len([]rune(*req.Location)) > locationMaxLen {
		fields["location"] = "Location must be at most 100 characters."
	}
	if req.Website != nil && strings.TrimSpace(*req.Website) != "" && !isValidHTTPURL(strings.TrimSpace(*req.Website)) {
		fields["website"] = "Website must be a valid http(s) URL."
	}
	if req.Password != nil && len(*req.Password) < minPasswordLen {
		fields["password"] = "Password must be at least 10 characters."
	}
	return fields
}

// listUsers (GET /users, owner only) returns every user, oldest first, as
// {"users": [USER]}.
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}

	dtos := make([]userDTO, 0, len(users))
	for _, u := range users {
		dtos = append(dtos, toUserDTO(u))
	}
	middleware.WriteJSON(w, http.StatusOK, map[string][]userDTO{"users": dtos})
}

// createUser (POST /users, owner only) creates a new owner or editor.
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	fields := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		fields["name"] = "Name is required."
	}
	switch {
	case strings.TrimSpace(req.Email) == "":
		fields["email"] = "Email is required."
	case !isValidEmail(req.Email):
		fields["email"] = "Email must be a valid email address."
	}
	switch {
	case req.Password == "":
		fields["password"] = "Password is required."
	case len(req.Password) < minPasswordLen:
		fields["password"] = "Password must be at least 10 characters."
	}
	if !isValidRole(req.Role) {
		fields["role"] = "Role must be either 'owner' or 'editor'."
	}
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	user, err := h.svc.CreateUser(r.Context(), auth.NewUserInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
		Role:     req.Role,
	})
	if errors.Is(err, auth.ErrEmailTaken) {
		h.validationError(w, map[string]string{"email": "This email is already in use."})
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}

	h.writeUser(w, http.StatusCreated, user)
}

// updateUser (PATCH /users/{id}, owner only) applies a partial update. It
// guards the last active owner (400 "last_owner") and 404s an unknown id.
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		profilePatchBody
		Role   *string `json:"role"`
		Active *bool   `json:"active"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	fields := validateProfilePatch(req.profilePatchBody)
	if req.Role != nil && !isValidRole(*req.Role) {
		fields["role"] = "Role must be either 'owner' or 'editor'."
	}
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	user, err := h.svc.UpdateUser(r.Context(), id, auth.UpdateUserInput{
		Name:     req.Name,
		Slug:     req.Slug,
		Bio:      req.Bio,
		Website:  req.Website,
		Location: req.Location,
		Avatar:   req.Avatar,
		Role:     req.Role,
		Active:   req.Active,
		Password: req.Password,
	})
	switch {
	case errors.Is(err, auth.ErrSlugTaken):
		h.validationError(w, map[string]string{"slug": "This author slug is already in use."})
		return
	case errors.Is(err, store.ErrNotFound):
		if req.Avatar != nil && *req.Avatar != "" {
			h.validationError(w, map[string]string{"avatar": "Avatar media was not found."})
			return
		}
		middleware.WriteError(w, http.StatusNotFound, "not_found", "User not found.")
		return
	case errors.Is(err, auth.ErrLastOwner):
		middleware.WriteError(w, http.StatusBadRequest, "last_owner", "Cannot deactivate or demote the last active owner.")
		return
	case err != nil:
		h.serverError(w, err)
		return
	}

	h.writeUser(w, http.StatusOK, user)
}

// isValidRole reports whether role is one of the two allowed roles.
func isValidRole(role string) bool {
	return role == store.RoleOwner || role == store.RoleEditor
}
