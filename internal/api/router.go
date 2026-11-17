package api

import (
	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

func NewRouter(users *service.UserService, auth *service.AuthService) *gin.Engine {
	userHandler := &userHandler{users: users}
	authHandler := &authHandler{auth: auth}
	friendHandler := &friendHandler{}
	postHandler := &postHandler{}

	router := gin.New()
	router.Use(gin.Logger(), recovery())

	// AUTH
	router.POST("/login", authHandler.login)
	router.POST("/user/register", userHandler.register)
	router.GET("/user/get/:id", userHandler.get)

	// SEARCH
	router.GET("/user/search", userHandler.search)

	// FRIENDS
	router.PUT("/friend/set/:user_id", friendHandler.set)
	router.PUT("/friend/delete/:user_id", friendHandler.deleteFriend)

	// POSTS
	router.POST("/post/create", postHandler.create)
	router.PUT("/post/update", postHandler.update)
	router.PUT("/post/delete/:id", postHandler.deletePost)
	router.GET("/post/get/:id", postHandler.get)
	router.GET("/post/feed", postHandler.feed)

	router.NoRoute(func(ctx *gin.Context) {
		writeError(ctx, domain.ErrRouteNotFound)
	})

	return router
}
