package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/salesarena/backend/internal/seed"
	"github.com/salesarena/backend/internal/service"
)

// fail maps service errors to HTTP statuses.
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		abort(c, http.StatusUnauthorized, "Invalid email or password.")
	case errors.Is(err, service.ErrWrongRole):
		role := strings.TrimPrefix(err.Error(), service.ErrWrongRole.Error()+": ")
		abort(c, http.StatusForbidden, "This account is a "+role+" account. Switch to the "+role+" tab to sign in.")
	case errors.Is(err, service.ErrNotFound):
		abort(c, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrBadInput):
		abort(c, http.StatusBadRequest, err.Error())
	default:
		slog.Error("request failed", "path", c.FullPath(), "err", err)
		abort(c, http.StatusInternalServerError, "internal error")
	}
}

// ---------- auth ----------

type loginReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role" binding:"required,oneof=BDA BDM"`
}

func (a *API) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "email, password and role (BDA|BDM) are required")
		return
	}
	res, err := a.svc.Login(c.Request.Context(), req.Email, req.Password, req.Role)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a *API) logout(c *gin.Context) {
	token, _ := c.Get("token")
	if t, ok := token.(string); ok {
		_ = a.svc.Logout(c.Request.Context(), t)
	}
	c.Status(http.StatusNoContent)
}

func (a *API) me(c *gin.Context) {
	s := session(c)
	u, err := a.svc.UserByID(c.Request.Context(), s.UserID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// ---------- BDA ----------

func (a *API) bdaOverview(c *gin.Context) {
	out, err := a.svc.Overview(c.Request.Context(), c.Param("id"), c.Query("range"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (a *API) bdaActivity(c *gin.Context) {
	out, err := a.svc.Activity(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type callsReq struct {
	Calls   int    `json:"calls"`
	Minutes int    `json:"minutes"`
	Date    string `json:"date"`
}

func (a *API) recordCalls(c *gin.Context) {
	var req callsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "body must be {calls, minutes, date?}")
		return
	}
	out, err := a.svc.RecordCalls(c.Request.Context(), c.Param("id"), req.Date, req.Calls, req.Minutes)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"day": out})
}

type leadReq struct {
	Outcome string `json:"outcome" binding:"required"`
	Company string `json:"company"`
	Date    string `json:"date"`
}

func (a *API) recordLead(c *gin.Context) {
	var req leadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "body must be {outcome: won|dropped, company?, date?}")
		return
	}
	out, err := a.svc.RecordLead(c.Request.Context(), c.Param("id"), req.Date, req.Outcome, req.Company)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"event": out})
}

// ---------- leaderboard & team ----------

func (a *API) leaderboard(c *gin.Context) {
	out, err := a.svc.Leaderboard(c.Request.Context(), c.Query("range"), c.Query("q"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (a *API) teamOverview(c *gin.Context) {
	out, err := a.svc.TeamOverview(c.Request.Context(), c.Query("range"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (a *API) dailyReport(c *gin.Context) {
	out, err := a.svc.DailyReport(c.Request.Context(), c.Query("date"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (a *API) noticeBoard(c *gin.Context) {
	out, err := a.svc.NoticeBoard(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ---------- roster admin (manager only) ----------

func (a *API) roster(c *gin.Context) {
	out, err := a.svc.Roster(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type createBdaReq struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (a *API) createBda(c *gin.Context) {
	var req createBdaReq
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "name, email and password are required")
		return
	}
	u, err := a.svc.CreateBDA(c.Request.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u})
}

// ---------- demo ----------

func (a *API) demoReset(c *gin.Context) {
	if err := a.svc.ResetDemo(c.Request.Context(), seed.Run); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
