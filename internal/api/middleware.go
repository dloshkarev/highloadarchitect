package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
				Message:   "Внутренняя ошибка сервера",
				RequestID: requestID,
				Code:      errorCodePanic,
			})
		}()
		ctx.Next()
	}
}
