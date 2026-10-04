package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

const (
	errorCodeInternal = 1000
	errorCodeDatabase = 1001
	errorCodePanic    = 1002
)

type serverError struct {
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Code      int    `json:"code,omitempty"`
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		ctx.AbortWithStatus(http.StatusBadRequest)
	case errors.Is(err, domain.ErrNotFound):
		ctx.AbortWithStatus(http.StatusNotFound)
	default:
		requestID := uuid.NewString()
		slog.Error("request failed", "err", err, "request_id", requestID, "path", ctx.FullPath())
		code := errorCodeInternal
		if _, ok := errors.AsType[*domain.DBError](err); ok {
			code = errorCodeDatabase
		}
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, serverError{
			Message:   "Внутренняя ошибка сервера",
			RequestID: requestID,
			Code:      code,
		})
	}
}
