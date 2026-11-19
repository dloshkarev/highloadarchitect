package api

import (
	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

func NewRouter(
	users *service.UserService,
	auth *service.AuthService,
	friends *service.FriendService,
	posts *service.PostService,
) *gin.Engine {
	userHandler := &userHandler{users: users}
	authHandler := &authHandler{auth: auth}
	friendHandler := &friendHandler{friends: friends}
	postHandler := &postHandler{posts: posts}

	router := gin.New()
	router.Use(gin.Logger(), recovery())

	// AUTH
	router.POST("/login", authHandler.login)
	router.POST("/user/register", userHandler.register)
	router.GET("/user/get/:id", userHandler.get)

	// SEARCH
	router.GET("/user/search", userHandler.search)

	// Дальше все ручки работают от имени текущего пользователя.
	authorized := router.Group("", authRequired(auth))

	// FRIENDS
	authorized.PUT("/friend/set/:user_id", friendHandler.set)
	authorized.PUT("/friend/delete/:user_id", friendHandler.deleteFriend)

	// POSTS
	authorized.POST("/post/create", postHandler.create)
	authorized.PUT("/post/update", postHandler.update)
	authorized.PUT("/post/delete/:id", postHandler.deletePost)
	authorized.GET("/post/get/:id", postHandler.get)
	authorized.GET("/post/feed", postHandler.feed)

	router.NoRoute(func(ctx *gin.Context) {
		writeError(ctx, domain.ErrRouteNotFound)
	})

	return router
}
