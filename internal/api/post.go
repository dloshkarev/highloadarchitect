package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

type postHandler struct {
	posts *service.PostService
}

type postResponse struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	AuthorUserID string `json:"author_user_id"`
}

type createPostRequest struct {
	Text string `json:"text"`
}

type createPostResponse struct {
	ID string `json:"id"`
}

type updatePostRequest struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// create создаёт пост текущего пользователя.
func (h *postHandler) create(ctx *gin.Context) {
	var req createPostRequest
	if !bindJSON(ctx, &req) {
		return
	}

	postID, err := h.posts.Create(ctx.Request.Context(), currentUserID(ctx), req.Text)
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.JSON(http.StatusOK, createPostResponse{ID: postID})
}

// update обновляет текст поста. Менять пост может только его автор.
func (h *postHandler) update(ctx *gin.Context) {
	var req updatePostRequest
	if !bindJSON(ctx, &req) {
		return
	}

	err := h.posts.Update(ctx.Request.Context(), currentUserID(ctx), req.ID, req.Text)
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.Status(http.StatusOK)
}

// deletePost удаляет пост. Удалять пост может только его автор.
func (h *postHandler) deletePost(ctx *gin.Context) {
	err := h.posts.Delete(ctx.Request.Context(), currentUserID(ctx), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.Status(http.StatusOK)
}

// get возвращает пост по идентификатору.
func (h *postHandler) get(ctx *gin.Context) {
	post, err := h.posts.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)

		return
	}
	ctx.JSON(http.StatusOK, makePostResponse(post))
}

// feed возвращает ленту постов друзей. Заглушка: логика появится позже.
func (*postHandler) feed(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, []postResponse{})
}

func makePostResponse(post domain.Post) postResponse {
	return postResponse{
		ID:           post.ID,
		Text:         post.Text,
		AuthorUserID: post.AuthorID,
	}
}
