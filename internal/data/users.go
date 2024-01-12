package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github/greenlight/internal/validator"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrDuplicateEmail = errors.New("duplicate email")
)

var AnonymousUser = &User{}

type User struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Password  password  `json:"-"`
	Activated bool      `json:"activated"`
	Version   int       `json:"Version"`
}

type password struct {
	plaintext *string
	hash      []byte
}

func (p *password) Set(plaintextPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), 12)
	if err != nil {
		return err
	}
	p.hash = hash
	p.plaintext = &plaintextPassword
	return nil
}

func (p *password) Match(plaintext string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(p.hash), []byte(plaintext))
	if err != nil {
		switch {
		case errors.Is(bcrypt.ErrMismatchedHashAndPassword, err):
			return false, nil
		default:
			return false, err
		}
	}
	return true, nil
}

func ValidateEmail(v *validator.Validator, email string) {
	v.Check(email != "", "email", "must be provided")
	v.Check(validator.Matches(email, validator.EmailRX), "email", "must be a valid email address")
}

func ValidatePasswordPlaintext(v *validator.Validator, password string) {
	v.Check(password != "", "password", "must be provided")
	v.Check(len(password) >= 8, "password", "must be at least 8 bytes long")
	v.Check(len(password) <= 72, "password", "must not be more than 72 bytes long")
}

func ValidateUser(v *validator.Validator, user *User) {
	v.Check(user.Name != "", "name", "must be provided")
	v.Check(len(user.Name) <= 500, "name", "must not be more than 500 bytes long")
	ValidateEmail(v, user.Email)
	if user.Password.plaintext != nil {
		ValidatePasswordPlaintext(v, *user.Password.plaintext)
	}
	if user.Password.hash == nil {
		panic("missing password hash for user")
	}
}

type UserModel struct {
	DB *sql.DB
}

func (m UserModel) Insert(u *User) error {
	stmt := `INSERT INTO users 
	(name, email, password_hash, activated) VALUES
	($1, $2, $3, $4)
	RETURNING id, version;
	`
	args := []any{u.Name, u.Email, u.Password.hash, u.Activated}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	row := m.DB.QueryRowContext(ctx, stmt, args...)
	if err := row.Scan(&u.ID, &u.Version); err != nil {
		switch {
		case err.Error() == `pq: duplicate key value violates unique constraint "users_email_key"`:
			return ErrDuplicateEmail
		default:
			return err
		}
	}
	return nil
}

func (m UserModel) GetByEmail(email string) (*User, error) {
	stmt := `SELECT id, created_at, name, email, password_hash, activated, version
	FROM users
	WHERE email = $1
	 `
	var user User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, stmt, email).Scan(
		&user.ID,
		&user.CreatedAt,
		&user.Name,
		&user.Email,
		&user.Password.hash,
		&user.Activated,
		&user.Version,
	)
	if err != nil {
		switch {
		case errors.Is(sql.ErrNoRows, err):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &user, nil
}

func (m UserModel) Update(user *User) error {
	stmt := `UPDATE users 
	SET name = $1 , email = $2, password_hash = $3,activated = $4,
	version = version + 1
	WHERE id = $5 AND version=$6
	RETURNING version
	 `
	args := []any{user.Name, user.Email, user.Password.hash, user.Activated, user.ID, user.Version}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.DB.QueryRowContext(ctx, stmt, args...).Scan(&user.Version)
	if err != nil {
		switch {
		case err.Error() == `pq: duplicate key value violates unique constraint "users_email_key"`:
			return ErrDuplicateEmail
		case errors.Is(sql.ErrNoRows, err):
			return ErrEditConflict
		default:
			return err
		}
	}
	return err
}

func (m UserModel) GetForToken(tokenScope string, tokenPlaintext string) (*User, error) {
	tokenHash := sha256.Sum256([]byte(tokenPlaintext))
	stmt := `SELECT users.id, users.created_at, users.name, users.email, users.password_hash, users.activated, users.version
	FROM users
	INNER JOIN tokens
	ON users.id = tokens.user_id
	WHERE tokens.hash = $1
	AND tokens.scope = $2
	AND tokens.expiry > $3
	`
	args := []any{tokenHash[:], tokenScope, time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var user User
	err := m.DB.QueryRowContext(ctx, stmt, args...).Scan(&user.ID,
		&user.CreatedAt,
		&user.Name,
		&user.Email,
		&user.Password.hash,
		&user.Activated,
		&user.Version,
	)
	if err != nil {
		switch {
		case errors.Is(sql.ErrNoRows, err):
			return nil, ErrRecordNotFound
		default:
			fmt.Println(err.Error())
			return nil, err
		}
	}
	return &user, nil
}

func (u *User) IsAnonymous() bool {
	return u == AnonymousUser
}