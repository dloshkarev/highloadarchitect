package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/service"
)

func NewRouter(users *service.UserService, auth *service.AuthService) *gin.Engine {
	userHandler := &userHandler{users: users}
	authHandler := &authHandler{auth: auth}

	router := gin.New()
	router.Use(gin.Logger(), recovery())
	router.POST("/login", authHandler.login)
	router.POST("/user/register", userHandler.register)
	router.GET("/user/get/:id", userHandler.get)
	router.NoRoute(func(ctx *gin.Context) {
		ctx.AbortWithStatus(http.StatusNotFound)
	})

	return router
}
