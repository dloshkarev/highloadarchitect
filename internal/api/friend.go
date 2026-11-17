package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type friendHandler struct{}

// set добавляет пользователя в друзья. Заглушка: логика появится позже.
func (*friendHandler) set(ctx *gin.Context) {
	ctx.Status(http.StatusOK)
}

// deleteFriend удаляет пользователя из друзей. Заглушка: логика появится позже.
func (*friendHandler) deleteFriend(ctx *gin.Context) {
	ctx.Status(http.StatusOK)
}
