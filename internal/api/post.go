package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type postHandler struct{}

type postResponse struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	AuthorUserID string `json:"author_user_id"`
}

type postIDResponse string

// create создаёт пост. Заглушка: логика появится позже.
func (*postHandler) create(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, postIDResponse(""))
}

// update обновляет пост. Заглушка: логика появится позже.
func (*postHandler) update(ctx *gin.Context) {
	ctx.Status(http.StatusOK)
}

// deletePost удаляет пост. Заглушка: логика появится позже.
func (*postHandler) deletePost(ctx *gin.Context) {
	ctx.Status(http.StatusOK)
}

// get возвращает пост. Заглушка: логика появится позже.
func (*postHandler) get(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, postResponse{})
}

// feed возвращает ленту постов друзей. Заглушка: логика появится позже.
func (*postHandler) feed(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, []postResponse{})
}
