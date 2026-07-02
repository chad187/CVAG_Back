package main

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestGenerateStateString(t *testing.T) {
	state1, err1 := generateStateString()
	state2, err2 := generateStateString()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotEmpty(t, state1)
	assert.NotEmpty(t, state2)
	assert.NotEqual(t, state1, state2) // Should be unique
	assert.Len(t, state1, 24)          // base64 encoded 16 bytes
}

func TestCreateJWT(t *testing.T) {
	user := User{
		ID:       primitive.NewObjectID(),
		Email:    "test@example.com",
		Name:     "Test User",
		Provider: "google",
	}

	token, err := createJWT(user)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify token can be parsed
	claims, err := parseJWT(token)
	assert.NoError(t, err)
	assert.Equal(t, user.ID.Hex(), claims.UserID)
	assert.Equal(t, user.Email, claims.Email)
}

func TestParseJWT(t *testing.T) {
	// Test valid token
	user := User{
		ID:    primitive.NewObjectID(),
		Email: "test@example.com",
	}
	token, _ := createJWT(user)

	claims, err := parseJWT(token)
	assert.NoError(t, err)
	assert.Equal(t, user.ID.Hex(), claims.UserID)
	assert.Equal(t, user.Email, claims.Email)

	// Test invalid token
	_, err = parseJWT("invalid.token.here")
	assert.Error(t, err)

	// Test expired token (create with past expiry)
	expiredClaims := AuthClaims{
		UserID: user.ID.Hex(),
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	expiredTokenString, _ := expiredToken.SignedString([]byte(jwtSecret))

	_, err = parseJWT(expiredTokenString)
	assert.Error(t, err)
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		header   string
		expected string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"Bearer abc123 extra", "abc123 extra"},
		{"", ""},
		{"Basic abc123", ""},
		{"Bearer", ""},
		{"bearer", ""},
	}

	for _, test := range tests {
		result := getBearerToken(test.header)
		assert.Equal(t, test.expected, result)
	}
}
