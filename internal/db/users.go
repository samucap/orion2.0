package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrUserNotFound is returned when a user lookup finds no matching row.
var ErrUserNotFound = errors.New("user not found")

// ErrEmailAlreadyExists is returned when creating/updating a user would violate
// the unique email constraint.
var ErrEmailAlreadyExists = errors.New("email already exists")

// User represents a row in the users table.
type User struct {
	ID        int64
	Email     string
	PW        string
	LastLogin *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	Avatar    string
}

// UserStore defines the persistence operations needed by the auth/user handlers.
// Tests can swap out db.Users with a mock implementation.
type UserStore interface {
	CreateUser(ctx context.Context, email, pw string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, userID int64) (*User, error)
	UpdateLastLogin(ctx context.Context, userID int64) error
	UpdateUserEmail(ctx context.Context, userID int64, newEmail string) error
	UpdateUserPassword(ctx context.Context, userID int64, newHashedPW string) error
	DeleteUser(ctx context.Context, userID int64) error
}

// PgUserStore is the default UserStore implementation backed by Postgres.
type PgUserStore struct{}

// Users is the active user store. Override in unit tests.
var Users UserStore = PgUserStore{}

func (PgUserStore) CreateUser(ctx context.Context, email, pw string) (*User, error) {
	return CreateUser(ctx, email, pw)
}
func (PgUserStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return GetUserByEmail(ctx, email)
}
func (PgUserStore) GetUserByID(ctx context.Context, userID int64) (*User, error) {
	return GetUserByID(ctx, userID)
}
func (PgUserStore) UpdateLastLogin(ctx context.Context, userID int64) error {
	return UpdateLastLogin(ctx, userID)
}
func (PgUserStore) UpdateUserEmail(ctx context.Context, userID int64, newEmail string) error {
	return UpdateUserEmail(ctx, userID, newEmail)
}
func (PgUserStore) UpdateUserPassword(ctx context.Context, userID int64, newHashedPW string) error {
	return UpdateUserPassword(ctx, userID, newHashedPW)
}
func (PgUserStore) DeleteUser(ctx context.Context, userID int64) error {
	return DeleteUser(ctx, userID)
}

// CreateUser inserts a new user and returns the created record.
// The password must already be hashed by the caller.
func CreateUser(ctx context.Context, email, pw string) (*User, error) {
	if Pool == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	q := `
		INSERT INTO users (email, pw)
		VALUES ($1, $2)
		RETURNING id, email, COALESCE(avatar, '')`

	var u User
	err := Pool.QueryRow(ctx, q, email, pw).Scan(&u.ID, &u.Email, &u.Avatar)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" { // unique violation
			return nil, ErrEmailAlreadyExists
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &u, nil
}

// GetUserByEmail looks up a user by email address.
// Returns ErrUserNotFound when no row matches.
func GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if Pool == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	q := `SELECT id, email, pw, COALESCE(avatar, '')
		FROM users
		WHERE email = $1`

	var u User
	err := Pool.QueryRow(ctx, q, email).Scan(&u.ID, &u.Email, &u.PW, &u.Avatar)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}

		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}

	return &u, nil
}

// GetUserByID looks up a user by ID.
// Returns ErrUserNotFound when no row matches.
func GetUserByID(ctx context.Context, userID int64) (*User, error) {
	if Pool == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	q := `SELECT id, email, pw, last_login, created_at, updated_at, COALESCE(avatar, '')
		FROM users
		WHERE id = $1`

	var u User
	err := Pool.QueryRow(ctx, q, userID).Scan(
		&u.ID,
		&u.Email,
		&u.PW,
		&u.LastLogin,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.Avatar,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}

	return &u, nil
}

// UpdateLastLogin sets last_login to the current time for the given user ID.
func UpdateLastLogin(ctx context.Context, userID int64) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}

	q := `UPDATE users SET last_login = NOW() WHERE id = $1`
	_, err := Pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	return nil
}

// UpdateUserEmail updates a user's email.
func UpdateUserEmail(ctx context.Context, userID int64, newEmail string) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}

	q := `UPDATE users SET email = $1 WHERE id = $2`
	res, err := Pool.Exec(ctx, q, newEmail, userID)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" { // unique violation
			return ErrEmailAlreadyExists
		}
		return fmt.Errorf("failed to update user email: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateUserPassword updates a user's password hash.
func UpdateUserPassword(ctx context.Context, userID int64, newHashedPW string) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}

	q := `UPDATE users SET pw = $1 WHERE id = $2`
	res, err := Pool.Exec(ctx, q, newHashedPW, userID)
	if err != nil {
		return fmt.Errorf("failed to update user password: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// DeleteUser deletes a user account by ID.
func DeleteUser(ctx context.Context, userID int64) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}

	q := `DELETE FROM users WHERE id = $1`
	res, err := Pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}
