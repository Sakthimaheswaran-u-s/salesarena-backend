// Package api wires HTTP routes to the service layer.
package api

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/salesarena/backend/internal/config"
	"github.com/salesarena/backend/internal/service"
)

type API struct {
	svc *service.Service
	cfg config.Config
}

func New(svc *service.Service, cfg config.Config) *API { return &API{svc: svc, cfg: cfg} }

func (a *API) Router() *gin.Engine {
	gin.SetMode(a.cfg.GinMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     a.cfg.CORSOrigins,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true, "time": time.Now()}) })

	v := r.Group("/api")
	{
		v.POST("/auth/login", a.login)
		if a.cfg.DemoMode {
			v.POST("/demo/reset", a.demoReset)
		}

		authed := v.Group("", a.requireAuth())
		{
			authed.POST("/auth/logout", a.logout)
			authed.GET("/auth/me", a.me)
			authed.GET("/leaderboard", a.leaderboard)
			authed.GET("/notice-board", a.noticeBoard)

			bda := authed.Group("/bda/:id", requireSelfOrBDM())
			{
				bda.GET("/overview", a.bdaOverview)
				bda.GET("/activity", a.bdaActivity)
				bda.POST("/calls", a.recordCalls)
				bda.POST("/leads", a.recordLead)
			}

			team := authed.Group("/team", requireRole("BDM"))
			{
				team.GET("/overview", a.teamOverview)
				team.GET("/daily", a.dailyReport)
			}

			admin := authed.Group("/bdas", requireRole("BDM"))
			{
				admin.GET("", a.roster)
				admin.POST("", a.createBda)
			}
		}
	}
	return r
}
