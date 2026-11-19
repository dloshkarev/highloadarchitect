package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

const (
	currentUserIDKey    = "currentUserID"
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
)

func recovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			requestID := uuid.NewString()
			slog.Error("panic", "err", rec, "request_id", requestID, "path", ctx.FullPath())
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, serverError{
				Message:   fmt.Sprintf("внутренняя ошибка сервера: %v", rec),
				RequestID: requestID,
				Code:      errorCodePanic,
			})
		}()
		ctx.Next()
	}
}

// authRequired проверяет Bearer-токен сессии и кладёт идентификатор текущего
// пользователя в контекст gin. Без валидного токена запрос получает 401.
func authRequired(auth *service.AuthService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, ok := bearerToken(ctx.GetHeader(authorizationHeader))
		if !ok {
			writeError(ctx, domain.ErrUnauthorized)

			return
		}

		userID, err := auth.UserIDByToken(ctx.Request.Context(), token)
		if err != nil {
			writeError(ctx, err)

			return
		}
		ctx.Set(currentUserIDKey, userID)
		ctx.Next()
	}
}

// currentUserID возвращает идентификатор пользователя, установленный authRequired.
func currentUserID(ctx *gin.Context) string {
	return ctx.GetString(currentUserIDKey)
}

// bearerToken вырезает токен из заголовка Authorization вида "Bearer <token>".
func bearerToken(header string) (string, bool) {
	token, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok {
		return "", false
	}
	token = strings.TrimSpace(token)

	return token, token != ""
}
