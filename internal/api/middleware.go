package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type contextKey struct{}

func requestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := strings.TrimSpace(ctx.GetHeader("X-Request-ID"))
		if id == "" {
			id = uuid.NewString()
		}
		ctx.Header("X-Request-ID", id)
		ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), contextKey{}, id))
		ctx.Next()
	}
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)

	return id
}

func recovery() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			requestID := requestIDFrom(ctx.Request.Context())
			slog.Error("panic", "err", rec, "request_id", requestID, "path", ctx.FullPath())
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, serverError{
				Message:   "Внутренняя ошибка сервера",
				RequestID: requestID,
				Code:      internalErrorCode,
			})
		}()
		ctx.Next()
	}
}
