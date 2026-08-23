package middleware

import (
	"net/http"
	"strings"
	"time"

	"cloud-control-server/config"
	"cloud-control-server/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// Claims JWT claims
type Claims struct {
	UserID       uint64 `json:"user_id"`
	Username     string `json:"username"`
	TokenVersion uint64 `json:"token_version"`
	jwt.RegisteredClaims
}

// GenerateToken 生成JWT token
func GenerateToken(userID uint64, username string, versions ...uint64) (string, string, error) {
	cfg := config.App.JWT
	now := time.Now()
	var tokenVersion uint64 = 1
	if len(versions) > 0 && versions[0] > 0 {
		tokenVersion = versions[0]
	}

	// Access token
	claims := Claims{
		UserID:       userID,
		Username:     username,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(cfg.ExpireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "cloud-control",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(cfg.Secret))
	if err != nil {
		return "", "", err
	}

	// Refresh token (longer lived)
	refreshClaims := Claims{
		UserID:       userID,
		Username:     username,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "cloud-control-refresh",
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshStr, nil
}

// ParseToken 解析JWT token
func ParseToken(tokenStr string) (*Claims, error) {
	return parseToken(tokenStr, "cloud-control")
}

// ParseRefreshToken 只接受刷新 token，避免把 access token 当成刷新 token 使用。
func ParseRefreshToken(tokenStr string) (*Claims, error) {
	return parseToken(tokenStr, "cloud-control-refresh")
}

func parseToken(tokenStr string, expectedIssuer string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(config.App.JWT.Secret), nil
	}, jwt.WithIssuer(expectedIssuer))
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

// JWTAuth JWT认证中间件
func JWTAuth(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusOK, models.Response{Code: 401, Message: "未授权，请重新登录"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusOK, models.Response{Code: 401, Message: "认证格式错误"})
			c.Abort()
			return
		}

		claims, err := ParseToken(parts[1])
		if err != nil {
			c.JSON(http.StatusOK, models.Response{Code: 401, Message: "未授权，请重新登录"})
			c.Abort()
			return
		}
		if db != nil {
			var user models.User
			if err := db.Select("id", "username", "status", "token_version").First(&user, claims.UserID).Error; err != nil || user.Status != 1 || (user.TokenVersion != 0 && claims.TokenVersion != user.TokenVersion) {
				c.JSON(http.StatusUnauthorized, models.Response{Code: 401, Message: "用户不存在或已停用"})
				c.Abort()
				return
			}
		}

		// 将用户信息存入上下文
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

// GetUserID 从上下文获取用户ID
func GetUserID(c *gin.Context) uint64 {
	id, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	switch value := id.(type) {
	case uint64:
		return value
	case uint:
		return uint64(value)
	case int:
		if value > 0 {
			return uint64(value)
		}
	case float64:
		if value > 0 {
			return uint64(value)
		}
	}
	return 0
}

// GetUsername 从上下文获取用户名
func GetUsername(c *gin.Context) string {
	name, exists := c.Get("username")
	if !exists {
		return ""
	}
	value, _ := name.(string)
	return value
}

// IsSystemAdminUser 使用数据库中的角色判断管理员，不再依赖用户 ID 大小。
func IsSystemAdminUser(db *gorm.DB, userID uint64) bool {
	if db == nil || userID == 0 {
		return false
	}
	var count int64
	db.Table("user_roles ur").
		Joins("JOIN roles r ON r.id = ur.role_id").
		Where("ur.user_id = ? AND r.code = ?", userID, "system_admin").
		Count(&count)
	return count > 0
}
