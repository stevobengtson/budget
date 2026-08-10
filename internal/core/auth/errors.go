package auth

import "errors"

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrInvalidEmail       = errors.New("enter a valid email address")
	ErrSameEmail          = errors.New("that is already your email")
	ErrAccountDisabled    = errors.New("this account has been disabled")
)
