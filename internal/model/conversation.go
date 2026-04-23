package model

type Conversation struct {
	ID       string
	Title    string
	CreateAt float64
	Messages []Message
}

type Message struct {
	ID      string
	Role    string
	Content string
	Blocks  []Block
}

type BlockKind string

const (
	BlockParagraph BlockKind = "paragraph"
	BlockMath      BlockKind = "math"
	BlockImage     BlockKind = "image"
	BlockTable     BlockKind = "table"
	BlockCode      BlockKind = "code"
)

type Block struct {
	Kind  BlockKind
	Text  string
	Image *Image
	Table *Table
	Code  *Code
}

type Image struct {
	Alt string
	Src string
}

type Table struct {
	Headers []string
	Rows    [][]string
}

type Code struct {
	Language string
	Text     string
}
