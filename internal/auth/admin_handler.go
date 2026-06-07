package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type adminUserRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

func (h *Handler) AdminListUsers(c *gin.Context) {
	users := h.store.ListUsers(c.Query("keyword"), c.Query("role"), c.Query("status"))
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (h *Handler) AdminCreateUser(c *gin.Context) {
	var req adminUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.store.AdminCreateUser(AdminUserInput{
		Username:  req.Username,
		Password:  req.Password,
		Nickname:  req.Nickname,
		AvatarURL: req.AvatarURL,
		Role:      req.Role,
		Status:    req.Status,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) AdminGetUser(c *gin.Context) {
	detail, err := h.store.AdminGetUser(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handler) AdminUpdateUser(c *gin.Context) {
	var req adminUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.store.AdminUpdateUser(c.Param("user_id"), AdminUserInput{
		Nickname:  req.Nickname,
		AvatarURL: req.AvatarURL,
		Role:      req.Role,
		Status:    req.Status,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) AdminResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.AdminResetPassword(c.Param("user_id"), req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminDeleteUser(c *gin.Context) {
	if err := h.store.AdminSoftDeleteUser(c.Param("user_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
