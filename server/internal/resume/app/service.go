// 本文件定义 Resume CRUD 用例的应用服务骨架。
package app

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// MaxResumesPerUser 是单个用户允许保留的活动简历数量上限。
const MaxResumesPerUser = 3

// Service 编排简历持久化和原始文件存储，不直接依赖 GORM 或具体对象存储。
type Service struct {
	repository Repository
	storage    FileStorage
	ids        IDGenerator
}

// NewService 创建 Resume 应用服务。
func NewService(repository Repository, storage FileStorage, ids IDGenerator) (*Service, error) {
	// TODO(issue-320): 后续补充配置项和时钟依赖。
	if repository == nil || storage == nil || ids == nil {
		return nil, errors.New("resume: service dependencies are required")
	}
	return &Service{repository: repository, storage: storage, ids: ids}, nil
}

// Create 创建一份属于当前认证用户的新简历。
func (s *Service) Create(context.Context, requestcontext.Actor, CreateCommand) (resume.Resume, error) {
	// TODO(issue-320): 实现三份上限、文件保存、幂等和解析入队。
	return resume.Resume{}, NotImplementedError()
}

// List 列出当前认证用户拥有的活动简历。
func (s *Service) List(context.Context, requestcontext.Actor, ListQuery) ([]resume.Resume, error) {
	// TODO(issue-320): 实现游标分页和稳定排序。
	return nil, NotImplementedError()
}

// Get 获取当前认证用户指定简历及其当前内容修订。
func (s *Service) Get(context.Context, requestcontext.Actor, string) (Detail, error) {
	// TODO(issue-320): 实现所有权隔离查询和详情投影。
	return Detail{}, NotImplementedError()
}

// UpdateMetadata 修改指定简历的展示名称。
func (s *Service) UpdateMetadata(context.Context, requestcontext.Actor, UpdateMetadataCommand) (resume.Resume, error) {
	// TODO(issue-320): 实现字段校验和乐观锁更新。
	return resume.Resume{}, NotImplementedError()
}

// UpdateContent 手动保存指定简历的一次结构化内容修订。
func (s *Service) UpdateContent(context.Context, requestcontext.Actor, UpdateContentCommand) (resume.Revision, error) {
	// TODO(issue-320): 实现结构化内容校验和不可变修订创建。
	return resume.Revision{}, NotImplementedError()
}

// ReplaceFile 替换指定简历的原始 PDF 并重新进入解析队列。
func (s *Service) ReplaceFile(context.Context, requestcontext.Actor, ReplaceFileCommand) (resume.Resume, error) {
	// TODO(issue-320): 实现新旧对象切换、补偿删除和解析重新入队。
	return resume.Resume{}, NotImplementedError()
}

// GetContentURL 获取指定简历原始 PDF 的短时授权读取地址。
func (s *Service) GetContentURL(context.Context, requestcontext.Actor, string) (ContentURL, error) {
	// TODO(issue-320): 实现所有权校验和短时签名地址生成。
	return ContentURL{}, NotImplementedError()
}

// RetryParse 重新提交一份解析失败的简历。
func (s *Service) RetryParse(context.Context, requestcontext.Actor, string) error {
	// TODO(issue-320): 实现状态校验和幂等重新入队。
	return NotImplementedError()
}

// Delete 删除指定简历并启动原始文件清理流程。
func (s *Service) Delete(context.Context, requestcontext.Actor, DeleteCommand) error {
	// TODO(issue-320): 实现软删除、对象清理和失败恢复。
	return NotImplementedError()
}
