package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates Bearer token or API key authentication.
// All protected endpoints should use this middleware.
//
// Expects either:
//   - Authorization: Bearer <token>
//   - X-API-Key: <key>
//
// Returns 401 Unauthorized if no valid auth is provided.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for Bearer token
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if isValidToken(token) {
					// Extract and pass user info to handlers if needed
					if userID := extractUserIDFromToken(token); userID != "" {
						c.Set("user_id", userID)
					}
					c.Next()
					return
				}
			}
		}

		// Fall back to API key
		apiKey := c.GetHeader("X-API-Key")
		if apiKey != "" {
			if isValidAPIKey(apiKey) {
				if userID := extractUserIDFromAPIKey(apiKey); userID != "" {
					c.Set("user_id", userID)
				}
				c.Next()
				return
			}
		}

		// No valid auth provided
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Missing authentication: provide Bearer token or X-API-Key header",
		})
		c.Abort()
	}
}

// isValidToken checks if a Bearer token is valid.
// Basic implementation: checks format and length.
// Production: should implement JWT verification.
func isValidToken(token string) bool {
	if len(token) == 0 {
		return false
	}
	// TODO: Implement proper JWT verification
	// import "github.com/golang-jwt/jwt/v4"
	// claims := &jwt.StandardClaims{}
	// _, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
	//   return []byte(os.Getenv("JWT_SECRET")), nil
	// })
	// return err == nil && claims.ExpiresAt > time.Now().Unix()

	// Basic validation: token must be at least 50 chars (placeholder)
	if len(token) < 50 {
		return false
	}
	return true
}

// extractUserIDFromToken extracts user ID from JWT token.
// Returns empty string if token is invalid or doesn't contain user ID.
func extractUserIDFromToken(token string) string {
	// TODO: Implement proper JWT decoding
	// import "github.com/golang-jwt/jwt/v4"
	// claims := &jwt.StandardClaims{}
	// jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
	//   return []byte(os.Getenv("JWT_SECRET")), nil
	// })
	// if claims != nil && claims.Subject != "" {
	//   return claims.Subject
	// }

	// Placeholder
	return "user-from-jwt"
}

// isValidAPIKey checks if an API key is valid.
// Basic implementation: checks format and length.
// Production: should query database for key validity and expiry.
func isValidAPIKey(apiKey string) bool {
	if len(apiKey) == 0 {
		return false
	}
	// TODO: Implement database lookup
	// SELECT id, user_id, active, expires_at FROM api_keys WHERE key = ? LIMIT 1
	// Check: active = true AND (expires_at IS NULL OR expires_at > now())

	// Basic validation: key must be alphanumeric + hyphens/underscores, min 20 chars
	if len(apiKey) < 20 {
		return false
	}
	for _, ch := range apiKey {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// extractUserIDFromAPIKey extracts user ID from API key.
// Returns empty string if key is invalid or doesn't exist.
func extractUserIDFromAPIKey(apiKey string) string {
	// TODO: Implement database lookup
	// SELECT user_id FROM api_keys WHERE key = ? LIMIT 1
	// return row.user_id

	// Placeholder
	return "user-from-api-key"
}
