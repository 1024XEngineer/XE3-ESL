// Package transport 定义 Resume 模块的 Gin HTTP 传输边界和受保护路由。
package transport

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

// Application 定义 Resume HTTP Handler 可以调用的应用用例。
type Application interface {
	// Create 创建当前用户的一份新简历。
	Create(context.Context, requestcontext.Actor, app.CreateCommand) (resume.Resume, error)
	// List 列出当前用户拥有的活动简历。
	List(context.Context, requestcontext.Actor, app.ListQuery) ([]resume.Resume, error)
	// Get 获取当前用户指定简历的详情。
	Get(context.Context, requestcontext.Actor, string) (app.Detail, error)
	// UpdateMetadata 修改当前用户指定简历的元数据。
	UpdateMetadata(context.Context, requestcontext.Actor, app.UpdateMetadataCommand) (resume.Resume, error)
	// UpdateContent 保存当前用户指定简历的手动内容修订。
	UpdateContent(context.Context, requestcontext.Actor, app.UpdateContentCommand) (resume.Revision, error)
	// ReplaceFile 替换当前用户指定简历的原始 PDF。
	ReplaceFile(context.Context, requestcontext.Actor, app.ReplaceFileCommand) (resume.Resume, error)
	// GetContentURL 获取当前用户指定简历的短时读取地址。
	GetContentURL(context.Context, requestcontext.Actor, string) (app.ContentURL, error)
	// RetryParse 重新提交当前用户指定简历的解析任务。
	RetryParse(context.Context, requestcontext.Actor, string) error
	// Delete 删除当前用户指定的简历。
	Delete(context.Context, requestcontext.Actor, app.DeleteCommand) error
}

// HTTPHandler 接收已通过 Identity 认证中间件的 Resume HTTP 请求。
type HTTPHandler struct {
	application Application
	errors      *httpresponse.Renderer
}

// NewHTTPHandler 创建 Resume HTTP Handler。
func NewHTTPHandler(application Application) (*HTTPHandler, error) {
	// TODO(issue-320): 后续注入上传限制、游标签名和读取地址配置。
	if application == nil {
		return nil, errors.New("resume: HTTP application is required")
	}
	return &HTTPHandler{
		application: application,
		errors:      httpresponse.NewRenderer(nil),
	}, nil
}

// RegisterRoutes 注册 Resume 模块的 REST 路由；调用方必须把它挂载到 Identity 认证路由组。
func (h *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	// TODO(issue-320): 在实现阶段补充逐路由上传大小限制和幂等中间件。
	routes.POST("/v1/resumes", h.create)
	routes.GET("/v1/resumes", h.list)
	routes.GET("/v1/resumes/:resume_id", h.get)
	routes.PATCH("/v1/resumes/:resume_id", h.updateMetadata)
	routes.PATCH("/v1/resumes/:resume_id/content", h.updateContent)
	routes.PUT("/v1/resumes/:resume_id/file", h.replaceFile)
	routes.GET("/v1/resumes/:resume_id/content-url", h.getContentURL)
	routes.POST("/v1/resumes/:resume_id/parse-retries", h.retryParse)
	routes.DELETE("/v1/resumes/:resume_id", h.delete)
}

// create 接收新简历 PDF 上传请求。
func (h *HTTPHandler) create(c *gin.Context) {
	// TODO(issue-320): 解析 multipart 请求并调用 Application.Create。
	h.writePlaceholder(c)
}

// list 返回当前用户拥有的简历列表。
func (h *HTTPHandler) list(c *gin.Context) {
	// TODO(issue-320): 解析分页参数并调用 Application.List。
	h.writePlaceholder(c)
}

// get 返回当前用户指定简历的元数据和当前结构化内容。
func (h *HTTPHandler) get(c *gin.Context) {
	// TODO(issue-320): 校验资源标识并调用 Application.Get。
	h.writePlaceholder(c)
}

// updateMetadata 修改指定简历的展示名称。
func (h *HTTPHandler) updateMetadata(c *gin.Context) {
	// TODO(issue-320): 解析 JSON 请求并调用 Application.UpdateMetadata。
	h.writePlaceholder(c)
}

// updateContent 手动保存指定简历的结构化内容修订。
func (h *HTTPHandler) updateContent(c *gin.Context) {
	// TODO(issue-320): 解析 JSON 请求并调用 Application.UpdateContent。
	h.writePlaceholder(c)
}

// replaceFile 替换指定简历的原始 PDF。
func (h *HTTPHandler) replaceFile(c *gin.Context) {
	// TODO(issue-320): 解析 multipart 请求并调用 Application.ReplaceFile。
	h.writePlaceholder(c)
}

// getContentURL 返回指定简历原始 PDF 的短时读取地址。
func (h *HTTPHandler) getContentURL(c *gin.Context) {
	// TODO(issue-320): 调用 Application.GetContentURL 并投影响应。
	h.writePlaceholder(c)
}

// retryParse 重新提交一份解析失败的简历。
func (h *HTTPHandler) retryParse(c *gin.Context) {
	// TODO(issue-320): 校验幂等键并调用 Application.RetryParse。
	h.writePlaceholder(c)
}

// delete 删除当前用户指定的简历。
func (h *HTTPHandler) delete(c *gin.Context) {
	// TODO(issue-320): 解析期望版本并调用 Application.Delete。
	h.writePlaceholder(c)
}

// writePlaceholder 验证可信 Actor 后返回尚未实现的统一应用错误。
func (h *HTTPHandler) writePlaceholder(c *gin.Context) {
	// TODO(issue-320): 各 Handler 完成后删除这个统一占位入口。
	if _, ok := requestcontext.ActorFromContext(c.Request.Context()); !ok {
		c.Header("WWW-Authenticate", "Bearer")
		h.errors.Write(c, apperror.New(
			apperror.Unauthenticated,
			"authentication_required",
			"Authentication is required.",
		))
		return
	}
	h.errors.Write(c, app.NotImplementedError())
}

var _ Application = (*app.Service)(nil)
