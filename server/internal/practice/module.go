// Package practice 管理练习计划、场次和推进策略
package practice

import (
	"errors"
	"fmt"
)

var ErrPracticeModuleDependencyMissing = errors.New("practice_module_dependency_missing")

// 只接收 Practice 的出站依赖，避免组装层泄漏其他模块实现
type Dependencies struct {
	PreparationReader  PreparationReader
	Repository         Repository
	TransactionManager TransactionManager
	IDGenerator        IDGenerator
	Clock              Clock
}

// 同时支持模块发现和显式依赖组装
type Module struct {
	service *Service
}

// 仅用于模块发现；需要调用应用服务时应使用 NewWithDependencies
func New() Module { return Module{} }

// 依赖缺失时拒绝构造，避免把不完整模块带到运行期
func NewWithDependencies(dependencies Dependencies) (Module, error) {
	checks := []struct {
		name    string
		missing bool
	}{
		{name: "PreparationReader", missing: dependencies.PreparationReader == nil},
		{name: "Repository", missing: dependencies.Repository == nil},
		{name: "TransactionManager", missing: dependencies.TransactionManager == nil},
		{name: "IDGenerator", missing: dependencies.IDGenerator == nil},
		{name: "Clock", missing: dependencies.Clock == nil},
	}
	for _, check := range checks {
		if check.missing {
			return Module{}, fmt.Errorf("%w: %s", ErrPracticeModuleDependencyMissing, check.name)
		}
	}
	return Module{service: newService(dependencies)}, nil
}

func (Module) Name() string { return "practice" }

func (m Module) Service() *Service { return m.service }
