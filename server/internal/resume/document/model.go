// Package document 把不同原始文件统一为字段提取器可消费的文档结构。
package document

// StructuredDocument 是供应商无关的解析结果。
type StructuredDocument struct {
	Format        string
	Markdown      string
	Pages         []Page
	ParserVersion string
}

// Page 表示原始文档中的一页。
type Page struct {
	Number int
	Blocks []Block
}

// Block 表示具有稳定顺序的文档内容块。
type Block struct {
	ID         string
	Type       string
	Text       string
	Page       int
	Confidence float64
}
