package auth

import (
	"net/http"
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

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name        string
		headerValue string
		expectError bool
		expected    string
	}{
		{
			name:        "valid token",
			headerValue: "Bearer abc123",
			expectError: false,
			expected:    "abc123",
		},
		{
			name:        "missing prefix",
			headerValue: "Token abc123",
			expectError: true,
		},
		{
			name:        "empty token",
			headerValue: "Bearer ",
			expectError: true,
		},
		{
			name:        "no header",
			headerValue: "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.headerValue != "" {
				headers.Set("Authorization", tt.headerValue)
			}

			token, err := GetBearerToken(headers)
			if (err != nil) != tt.expectError {
				t.Fatalf("expected error: %v, got: %v", tt.expectError, err)
			}
			if token != tt.expected {
				t.Fatalf("expected token: %q, got: %q", tt.expected, token)
			}
		})
	}
}
