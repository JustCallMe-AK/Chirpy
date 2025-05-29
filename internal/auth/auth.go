package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

func HashedPassword(password string) (string, error) {
	hashedPassword, hashingError := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if hashingError != nil {
		return password, errors.New("failed to hash password")
	} else {
		return string(hashedPassword), nil
	}
}

func CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
