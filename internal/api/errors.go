package api

import (
	"errors"
	"fmt"
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
	requestID := uuid.NewString()
	status, code := classify(err)
	slog.Error("request failed", "err", err, "status", status, "request_id", requestID, "path", ctx.FullPath())
	ctx.AbortWithStatusJSON(status, serverError{
		Message:   clientMessage(err),
		RequestID: requestID,
		Code:      code,
	})
}

func bindJSON(ctx *gin.Context, dst any) bool {
	if err := ctx.ShouldBindJSON(dst); err != nil {
		writeError(ctx, domain.NewValidationError(fmt.Sprintf("некорректное тело запроса: %s", err.Error())))

		return false
	}

	return true
}

func classify(err error) (int, int) {
	switch {
	case isValidation(err):
		return http.StatusBadRequest, 0
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrRouteNotFound):
		return http.StatusNotFound, 0
	default:
		code := errorCodeInternal
		if _, ok := errors.AsType[*domain.DBError](err); ok {
			code = errorCodeDatabase
		}

		return http.StatusInternalServerError, code
	}
}

func isValidation(err error) bool {
	_, ok := errors.AsType[*domain.ValidationError](err)

	return ok
}

func clientMessage(err error) string {
	validation, ok := errors.AsType[*domain.ValidationError](err)
	if ok {
		return validation.Error()
	}
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotFound.Error()
	}
	if errors.Is(err, domain.ErrRouteNotFound) {
		return domain.ErrRouteNotFound.Error()
	}

	return err.Error()
}
