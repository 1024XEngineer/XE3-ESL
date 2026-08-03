// 本文件定义简历业务模块在模块化单体健康信息中的最小标识。
package resume

// Module 是 Resume 业务模块的无状态标识。
type Module struct{}

// NewModule 创建 Resume 业务模块标识。
func NewModule() Module {
	// TODO(issue-320): 在真实运行时装配完成后补充需要暴露的模块能力。
	return Module{}
}

// Name 返回健康检查中使用的稳定模块名称。
func (Module) Name() string {
	// TODO(issue-320): 与运行时装配一起验证健康契约中的模块顺序。
	return "resume"
}
