package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/service"
)

type friendHandler struct {
	friends *service.FriendService
}

// set добавляет пользователя из пути в друзья текущего пользователя.
func (h *friendHandler) set(ctx *gin.Context) {
	if err := h.friends.Set(ctx.Request.Context(), currentUserID(ctx), ctx.Param("user_id")); err != nil {
		writeError(ctx, err)

		return
	}
	ctx.Status(http.StatusOK)
}

// deleteFriend удаляет пользователя из пути из друзей текущего пользователя.
func (h *friendHandler) deleteFriend(ctx *gin.Context) {
	if err := h.friends.Delete(ctx.Request.Context(), currentUserID(ctx), ctx.Param("user_id")); err != nil {
		writeError(ctx, err)

		return
	}
	ctx.Status(http.StatusOK)
}
