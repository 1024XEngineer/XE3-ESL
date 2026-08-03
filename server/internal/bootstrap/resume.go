// 本文件是 Resume 模块的组装入口，只负责连接依赖，不承载业务逻辑。
package bootstrap

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	resumeapp "github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
	resumepersistence "github.com/1024XEngineer/XE3-ESL/server/internal/resume/persistence"
	resumetransport "github.com/1024XEngineer/XE3-ESL/server/internal/resume/transport"
)

// ResumeConfiguration 保存 Resume 运行时需要的边界配置。
type ResumeConfiguration struct {
	MaximumFileBytes  int64
	ParsePollInterval time.Duration
	ReadURLLifetime   time.Duration
}

// ResumeComposition 聚合 Resume HTTP Handler 和后台解析 Worker。
type ResumeComposition struct {
	handler *resumetransport.HTTPHandler
	worker  *resumeapp.ParseWorker
}

// NewResumeComposition 使用进程依赖组装 Resume 模块。
func NewResumeComposition(
	pool *pgxpool.Pool,
	storage resumeapp.FileStorage,
	parser resumeapp.Parser,
	ids resumeapp.IDGenerator,
	configuration ResumeConfiguration,
) (*ResumeComposition, error) {
	// TODO(issue-320): 在真实实现 Issue 中应用配置并增加端到端组装测试。
	if pool == nil || storage == nil || parser == nil || ids == nil ||
		configuration.MaximumFileBytes <= 0 ||
		configuration.ParsePollInterval <= 0 ||
		configuration.ReadURLLifetime <= 0 {
		return nil, errors.New("bootstrap: Resume dependencies are required")
	}
	repository, err := resumepersistence.NewGormRepositoryFromPool(pool)
	if err != nil {
		return nil, err
	}
	service, err := resumeapp.NewService(repository, storage, ids)
	if err != nil {
		return nil, err
	}
	handler, err := resumetransport.NewHTTPHandler(service)
	if err != nil {
		return nil, err
	}
	worker, err := resumeapp.NewParseWorker(repository, storage, parser)
	if err != nil {
		return nil, err
	}
	return &ResumeComposition{handler: handler, worker: worker}, nil
}

// HTTPHandler 返回需要挂载到 Identity 认证路由组的 Resume Handler。
func (c *ResumeComposition) HTTPHandler() *resumetransport.HTTPHandler {
	// TODO(issue-320): 在 main 组装 Issue 中接入受保护路由。
	if c == nil {
		return nil
	}
	return c.handler
}

// Worker 返回由 server 进程托管生命周期的解析 Worker。
func (c *ResumeComposition) Worker() *resumeapp.ParseWorker {
	// TODO(issue-320): 在解析实现 Issue 中接入启动和优雅停止。
	if c == nil {
		return nil
	}
	return c.worker
}
