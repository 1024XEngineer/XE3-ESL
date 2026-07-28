package command

import "strings"

type Router struct {
	registry *Registry
}

// NewRouter 创建使用指定注册表的斜杠命令路由器。
func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
}

// Parse 把斜杠命令转换成工具调用，并忽略普通自然语言输入。
func (router *Router) Parse(input string) (Parsed, bool, error) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return Parsed{}, false, nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if body == "" {
		return Parsed{}, true, ErrInvalidCommand
	}
	name, args := splitCommand(body)
	definition, ok := router.registry.Get(name)
	if !ok {
		return Parsed{}, true, ErrUnknownCommand
	}
	raw, err := definition.BuildInput(args)
	if err != nil {
		return Parsed{}, true, err
	}
	return Parsed{
		CommandName: definition.Name,
		Args:        args,
		Invocation:  agentInvocation(definition.ToolName, raw),
	}, true, nil
}

// splitCommand 把命令正文拆成命令名和原始参数。
func splitCommand(body string) (string, string) {
	parts := strings.Fields(body)
	if len(parts) == 0 {
		return "", ""
	}
	name := parts[0]
	args := strings.TrimSpace(strings.TrimPrefix(body, name))
	return name, args
}
