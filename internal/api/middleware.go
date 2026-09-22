package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/salesarena/backend/internal/cache"
)

const ctxSession = "session"

// requireAuth validates the bearer token against Redis and stores the session in the context.
func (a *API) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			abort(c, http.StatusUnauthorized, "missing bearer token")
			return
		}
		sess, err := a.svc.Authenticate(c.Request.Context(), token)
		if err != nil {
			abort(c, http.StatusServiceUnavailable, "session store unavailable")
			return
		}
		if sess == nil {
			abort(c, http.StatusUnauthorized, "session expired or invalid")
			return
		}
		c.Set(ctxSession, sess)
		c.Set("token", token)
		c.Next()
	}
}

func session(c *gin.Context) *cache.Session {
	v, _ := c.Get(ctxSession)
	s, _ := v.(*cache.Session)
	return s
}

// requireRole allows only the listed roles.
func requireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := session(c)
		for _, r := range roles {
			if s != nil && s.Role == r {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "insufficient role")
	}
}

// requireSelfOrBDM lets a BDA touch only its own resources; BDMs can touch any.
func requireSelfOrBDM() gin.HandlerFunc {
	return func(c *gin.Context) {
		s := session(c)
		if s == nil || (s.Role != "BDM" && s.UserID != c.Param("id")) {
			abort(c, http.StatusForbidden, "you can only access your own data")
			return
		}
		c.Next()
	}
}

func abort(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}
