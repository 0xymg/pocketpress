package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pocketpress/pocketpress/internal/app"
	"github.com/pocketpress/pocketpress/internal/auth"
	"github.com/pocketpress/pocketpress/internal/store"
)

// minUserPasswordLen mirrors the admin API's minimum password length
// (internal/http/adminapi.minPasswordLen, unexported there), so
// `pocketpress user create` and `pocketpress user reset-password` enforce
// the exact same rule as the HTTP API rather than a second, drifting copy.
const minUserPasswordLen = 10

// newUserCmd builds the `user` subcommand tree: `user create` and
// `user reset-password`.
func newUserCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "user",
		Short: "Manage PocketPress admin users",
	}

	root.AddCommand(newUserCreateCmd())
	root.AddCommand(newUserResetPasswordCmd())

	return root
}

// openMigratedStore opens the store at the configured data directory and
// applies any pending migrations, exactly like `migrate up` does — the
// user subcommands work directly against the database (there is no running
// server to go through), so they must bring the schema current themselves
// before reading or writing user rows.
func openMigratedStore(cmd *cobra.Command) (*store.Store, error) {
	cfg := app.Load()

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if _, err := store.Up(cmd.Context(), st.DB()); err != nil {
		st.Close()
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}
	return st, nil
}

// newUserCreateCmd builds the `user create` subcommand: it validates its
// flags exactly like the admin API's POST /users handler (email format,
// password length, role enum, duplicate email) and delegates the actual
// creation — including password hashing — to auth.Service.
func newUserCreateCmd() *cobra.Command {
	var (
		email    string
		name     string
		password string
		role     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new admin user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if !isValidUserEmail(email) {
				return fmt.Errorf("--email must be a valid email address, got %q", email)
			}
			if len(password) < minUserPasswordLen {
				return fmt.Errorf("--password must be at least %d characters", minUserPasswordLen)
			}
			if !isValidUserRole(role) {
				return fmt.Errorf("--role must be %q or %q, got %q", store.RoleOwner, store.RoleEditor, role)
			}

			st, err := openMigratedStore(cmd)
			if err != nil {
				return err
			}
			defer st.Close()

			svc := auth.NewService(st.DB(), nil)
			user, err := svc.CreateUser(cmd.Context(), auth.NewUserInput{
				Name:     name,
				Email:    email,
				Password: password,
				Role:     role,
			})
			if errors.Is(err, auth.ErrEmailTaken) {
				return fmt.Errorf("a user with email %q already exists", email)
			}
			if err != nil {
				return err
			}

			cmd.Printf("Created user %s <%s> (role: %s)\n", user.Name, user.Email, user.Role)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&name, "name", "", "Display name (required)")
	cmd.Flags().StringVar(&password, "password", "", "Password, at least 10 characters (required)")
	cmd.Flags().StringVar(&role, "role", "", "Role: owner or editor (required)")
	return cmd
}

// newUserResetPasswordCmd builds the `user reset-password` subcommand. When
// --password is omitted it reads a replacement from stdin. Hashing is
// delegated to auth.Service.UpdateUser, reusing the same argon2id
// parameters as every other password write in PocketPress.
func newUserResetPasswordCmd() *cobra.Command {
	var (
		email    string
		password string
	)

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Reset an admin user's password",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isValidUserEmail(email) {
				return fmt.Errorf("--email must be a valid email address, got %q", email)
			}

			if password == "" {
				read, err := readPasswordFromStdin(cmd)
				if err != nil {
					return err
				}
				password = read
			}
			if len(password) < minUserPasswordLen {
				return fmt.Errorf("password must be at least %d characters", minUserPasswordLen)
			}

			st, err := openMigratedStore(cmd)
			if err != nil {
				return err
			}
			defer st.Close()

			user, err := store.GetUserByEmail(cmd.Context(), st.DB(), email)
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("no user found with email %q", email)
			}
			if err != nil {
				return err
			}

			svc := auth.NewService(st.DB(), nil)
			if _, err := svc.UpdateUser(cmd.Context(), user.ID, auth.UpdateUserInput{Password: &password}); err != nil {
				return err
			}

			cmd.Printf("Password reset for %s <%s>\n", user.Name, user.Email)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address of the user to update (required)")
	cmd.Flags().StringVar(&password, "password", "", "New password, at least 10 characters (omit to read one line from stdin)")
	return cmd
}

// readPasswordFromStdin reads a single line from stdin as the new password.
// This is a plain, unmasked read (no terminal echo suppression) since that
// would require an extra dependency beyond the stdlib; callers on an
// interactive terminal should be aware the value they type is visible.
func readPasswordFromStdin(cmd *cobra.Command) (string, error) {
	cmd.Println("Enter new password (input is not hidden):")
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return "", errors.New("read password: no input")
	}
	return strings.TrimRight(scanner.Text(), "\r\n"), nil
}

// isValidUserEmail mirrors internal/http/adminapi.isValidEmail: email must
// parse as a single, plain addr-spec (no display name, no angle brackets).
func isValidUserEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

// isValidUserRole mirrors internal/http/adminapi.isValidRole.
func isValidUserRole(role string) bool {
	return role == store.RoleOwner || role == store.RoleEditor
}
