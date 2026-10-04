package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

type userHandler struct {
	users *service.UserService
}

type registerRequest struct {
	FirstName  string `json:"first_name"`
	SecondName string `json:"second_name"`
	Birthdate  string `json:"birthdate"`
	Biography  string `json:"biography"`
	City       string `json:"city"`
	Password   string `json:"password"`
}

type registerResponse struct {
	UserID string `json:"user_id"`
}

type userResponse struct {
	ID         string `json:"id"`
	FirstName  string `json:"first_name"`
	SecondName string `json:"second_name"`
	Birthdate  string `json:"birthdate"`
	Biography  string `json:"biography"`
	City       string `json:"city"`
}

func (h *userHandler) register(ctx *gin.Context) {
	var req registerRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.AbortWithStatus(http.StatusBadRequest)

		return
	}

	userID, err := h.users.Register(ctx.Request.Context(), service.RegisterInput{
		FirstName:  req.FirstName,
		SecondName: req.SecondName,
		Birthdate:  req.Birthdate,
		Biography:  req.Biography,
		City:       req.City,
		Password:   req.Password,
	})
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.JSON(http.StatusOK, registerResponse{UserID: userID})
}

func (h *userHandler) get(ctx *gin.Context) {
	user, err := h.users.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.JSON(http.StatusOK, newUserResponse(user))
}

func newUserResponse(user domain.User) userResponse {
	return userResponse{
		ID:         user.ID,
		FirstName:  user.FirstName,
		SecondName: user.SecondName,
		Birthdate:  user.Birthdate.Format(time.DateOnly),
		Biography:  user.Biography,
		City:       user.City,
	}
}
