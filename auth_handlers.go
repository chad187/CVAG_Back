package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	googleOAuthConfig    *oauth2.Config
	microsoftOAuthConfig *oauth2.Config
	jwtSecret            string
	serverBaseURL        string
	frontendBaseURL      string
	stateCookieName      = "oauth_state"
	defaultJWTExpiry     = 24 * time.Hour
	googleUserInfoURL    = "https://openidconnect.googleapis.com/v1/userinfo"
	azureUserInfoURL     = "https://graph.microsoft.com/oidc/userinfo"
	isProduction         bool
)

func getEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func initAuth() {
	jwtSecret = os.Getenv("AUTH_JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("AUTH_JWT_SECRET environment variable is required")
	}

	isProduction = os.Getenv("IN_PRODUCTION") == "true"

	serverBaseURL = os.Getenv("AUTH_SERVER_BASE_URL")
	if serverBaseURL == "" {
		serverBaseURL = "http://localhost:8080"
	}

	frontendBaseURL = os.Getenv("AUTH_FRONTEND_BASE_URL")
	if frontendBaseURL == "" {
		frontendBaseURL = "http://localhost:5173"
	}

	googleClientID := getEnv("GOOGLE_CLIENT_ID", "GOOGLE_OAUTH_CLIENT_ID")
	googleClientSecret := getEnv("GOOGLE_CLIENT_SECRET", "GOOGLE_OAUTH_CLIENT_SECRET")
	if googleClientID == "" || googleClientSecret == "" {
		log.Fatal("Google OAuth client credentials are required: set GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET or GOOGLE_OAUTH_CLIENT_ID/GOOGLE_OAUTH_CLIENT_SECRET")
	}

	microsoftClientID := getEnv("MICROSOFT_CLIENT_ID", "MICROSOFT_OAUTH_CLIENT_ID")
	microsoftClientSecret := getEnv("MICROSOFT_CLIENT_SECRET", "MICROSOFT_OAUTH_CLIENT_SECRET")
	if microsoftClientID == "" || microsoftClientSecret == "" {
		log.Fatal("Microsoft OAuth client credentials are required: set MICROSOFT_CLIENT_ID/MICROSOFT_CLIENT_SECRET or MICROSOFT_OAUTH_CLIENT_ID/MICROSOFT_OAUTH_CLIENT_SECRET")
	}

	googleOAuthConfig = &oauth2.Config{
		ClientID:     googleClientID,
		ClientSecret: googleClientSecret,
		RedirectURL:  fmt.Sprintf("%s/auth/google/callback", strings.TrimRight(serverBaseURL, "/")),
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     google.Endpoint,
	}

	microsoftOAuthConfig = &oauth2.Config{
		ClientID:     microsoftClientID,
		ClientSecret: microsoftClientSecret,
		RedirectURL:  fmt.Sprintf("%s/auth/microsoft/callback", strings.TrimRight(serverBaseURL, "/")),
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		},
	}
}

func googleLogin(c *gin.Context) {
	state, err := generateStateString()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate oauth state"})
		return
	}
	setStateCookie(c, state)
	c.Redirect(http.StatusFound, googleOAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline))
}

func microsoftLogin(c *gin.Context) {
	state, err := generateStateString()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate oauth state"})
		return
	}
	setStateCookie(c, state)
	c.Redirect(http.StatusFound, microsoftOAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline))
}

func googleCallback(c *gin.Context) {
	handleOAuthCallback(c, "google", googleOAuthConfig)
}

func microsoftCallback(c *gin.Context) {
	handleOAuthCallback(c, "microsoft", microsoftOAuthConfig)
}

func handleOAuthCallback(c *gin.Context, provider string, config *oauth2.Config) {
	state := c.Query("state")
	if err := validateStateCookie(c, state); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid oauth state"})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}

	token, err := config.Exchange(c, code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to exchange code"})
		return
	}

	profile, err := fetchSocialProfile(c, provider, token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to fetch profile", "details": err.Error()})
		return
	}

	if profile.Email == "" {
		profile.Email = profile.PreferredUsername
	}
	if profile.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "social provider did not return an email"})
		return
	}

	user, err := findOrCreateSocialUser(c, provider, profile.Sub, profile.Email, profile.Name, profile.Picture)
	if err != nil {
		log.Printf("findOrCreateSocialUser error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user"})
		return
	}

	tokenString, err := createJWT(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create jwt"})
		return
	}

	// Redirect to frontend with token
	redirectURL := fmt.Sprintf("%s?token=%s", frontendBaseURL, tokenString)
	c.Redirect(http.StatusFound, redirectURL)
}

func authMe(c *gin.Context) {
	tokenString := getBearerToken(c.GetHeader("Authorization"))
	if tokenString == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}

	claims, err := parseJWT(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "details": err.Error()})
		return
	}

	userID, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
		return
	}

	user, err := GetUserByID(c, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

func generateStateString() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

func setStateCookie(c *gin.Context, state string) {
	c.SetCookie(stateCookieName, state, 300, "/", "", false, true)
}

func validateStateCookie(c *gin.Context, state string) error {
	stored, err := c.Cookie(stateCookieName)
	if err != nil {
		return err
	}
	if stored != state {
		return errors.New("state mismatch")
	}
	return nil
}

func fetchSocialProfile(ctx context.Context, provider string, token *oauth2.Token) (*socialProfile, error) {
	var url string
	if provider == "google" {
		url = googleUserInfoURL
	} else if provider == "microsoft" {
		url = azureUserInfoURL
	} else {
		return nil, fmt.Errorf("unsupported provider %s", provider)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed: %s", resp.Status)
	}

	var profile socialProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func findOrCreateSocialUser(ctx context.Context, provider, providerID, email, name, picture string) (User, error) {
	filter := bson.M{"provider": provider, "provider_id": providerID}

	// Generate the ID in Go upfront. If the user is new, this is their ID.
	// If they exist, MongoDB completely ignores this field.
	newID := primitive.NewObjectID()

	update := bson.M{
		"$set": bson.M{
			"email":      email,
			"name":       name,
			"picture":    picture,
			"last_login": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":         newID,
			"provider":    provider,
			"provider_id": providerID,
			"created_at":  time.Now(),
			"company_ids": []string{}, // Initializes as a clean, empty string slice
			"sys_admin":   false,
		},
	}

	// SetUpsert(true) creates the record if missing.
	// SetReturnDocument(options.After) returns the final document AFTER the write is done.
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var user User
	err := database.Collection("users").FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func createJWT(user User) (string, error) {
	claims := AuthClaims{
		UserID: user.ID.Hex(),
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.Hex(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(defaultJWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "remote-server",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
}

func parseJWT(tokenString string) (*AuthClaims, error) {
	claims := &AuthClaims{}
	parsed, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func getUser(c *gin.Context) (User, error) {
	tokenString := getBearerToken(c.GetHeader("Authorization"))
	if tokenString == "" {
		if !isProduction {
			return getDevUser(c)
		}
		return User{}, errors.New("missing token")
	}

	claims, err := parseJWT(tokenString)
	if err != nil {
		if !isProduction {
			return getDevUser(c)
		}
		return User{}, err
	}

	var user User
	objID, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		if !isProduction {
			return getDevUser(c)
		}
		return User{}, errors.New("invalid user ID format")
	}

	err = database.Collection("users").FindOne(c, bson.M{"_id": objID}).Decode(&user)
	if err != nil {
		if !isProduction {
			return getDevUser(c)
		}
		return User{}, errors.New("user not found")
	}
	return user, nil
}

func getDevUser(c *gin.Context) (User, error) {
	cursor, err := database.Collection("companies").Find(c, bson.M{})
	if err != nil {
		return User{}, err
	}
	defer cursor.Close(c)

	var companies []Company
	if err := cursor.All(c, &companies); err != nil {
		return User{}, err
	}

	companyIDs := make([]string, 0, len(companies))
	for _, comp := range companies {
		companyIDs = append(companyIDs, comp.ID)
	}

	return User{
		ID:         primitive.NewObjectID(),
		SysAdmin:   false,
		CompanyIDs: companyIDs,
		Email:      "dev@localhost",
		Name:       "Dev User",
		Provider:   "dev",
		ProviderID: "dev",
		CreatedAt:  time.Now(),
		LastLogin:  time.Now(),
	}, nil
}

func getBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
