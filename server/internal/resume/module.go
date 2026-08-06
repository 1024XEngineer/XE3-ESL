// 本文件定义简历业务模块在模块化单体健康信息中的最小标识。
package resume

// Module 是 Resume 业务模块的无状态标识。
type Module struct{}

// NewModule 创建 Resume 业务模块标识。
func NewModule() Module {
	return Module{}
}

// Name 返回健康检查中使用的稳定模块名称。
func (Module) Name() string {
	return "resume"
}
