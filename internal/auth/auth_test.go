package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeAndValidateJWT(t *testing.T) {
	secret := "test-secret"
	userID := uuid.New()

	token, err := MakeJWT(userID, secret, time.Minute)
	if err != nil {
		t.Fatalf("failed to create JWT: %v", err)
	}

	validatedID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("failed to validate JWT: %v", err)
	}

	if validatedID != userID {
		t.Errorf("expected userID %v, got %v", userID, validatedID)
	}
}

func TestExpiredJWT(t *testing.T) {
	secret := "test-secret"
	userID := uuid.New()

	// Token expires 1 second ago
	token, err := MakeJWT(userID, secret, -1*time.Second)
	if err != nil {
		t.Fatalf("failed to create expired JWT: %v", err)
	}

	_, err = ValidateJWT(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token, got none")
	}
}

func TestInvalidSignatureJWT(t *testing.T) {
	secret := "correct-secret"
	wrongSecret := "wrong-secret"
	userID := uuid.New()

	token, err := MakeJWT(userID, secret, time.Minute)
	if err != nil {
		t.Fatalf("failed to create JWT: %v", err)
	}

	_, err = ValidateJWT(token, wrongSecret)
	if err == nil {
		t.Fatal("expected error for token with invalid signature, got none")
	}
}
