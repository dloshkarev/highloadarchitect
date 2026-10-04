package api

import (
	"fmt"
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
				Message:   fmt.Sprintf("внутренняя ошибка сервера: %v", rec),
				RequestID: requestID,
				Code:      errorCodePanic,
			})
		}()
		ctx.Next()
	}
}
