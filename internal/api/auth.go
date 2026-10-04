package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/service"
)

type authHandler struct {
	auth *service.AuthService
}

type loginRequest struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *authHandler) login(ctx *gin.Context) {
	var req loginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.AbortWithStatus(http.StatusBadRequest)

		return
	}

	token, err := h.auth.Login(ctx.Request.Context(), req.ID, req.Password)
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.JSON(http.StatusOK, loginResponse{Token: token})
}
