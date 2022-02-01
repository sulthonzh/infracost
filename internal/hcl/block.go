package hcl

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	log "github.com/sirupsen/logrus"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

var terraformSchemaV012 = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type: "terraform",
		},
		{
			Type:       "provider",
			LabelNames: []string{"name"},
		},
		{
			Type:       "variable",
			LabelNames: []string{"name"},
		},
		{
			Type: "locals",
		},
		{
			Type:       "output",
			LabelNames: []string{"name"},
		},
		{
			Type:       "module",
			LabelNames: []string{"name"},
		},
		{
			Type:       "resource",
			LabelNames: []string{"type", "name"},
		},
		{
			Type:       "data",
			LabelNames: []string{"type", "name"},
		},
	},
}

type Blocks []*Block

func (blocks Blocks) OfType(t string) Blocks {
	var results []*Block

	for _, block := range blocks {
		if block.Type() == t {
			results = append(results, block)
		}
	}

	return results
}

type Block struct {
	hclBlock         *hcl.Block
	context          *Context
	moduleBlock      *Block
	expanded         bool
	cloneIndex       int
	childBlocks      Blocks
	cachedAttributes []*Attribute
}

func NewHCLBlock(hclBlock *hcl.Block, ctx *Context, moduleBlock *Block) *Block {
	if ctx == nil {
		ctx = NewContext(&hcl.EvalContext{}, nil)
	}

	var children Blocks
	if body, ok := hclBlock.Body.(*hclsyntax.Body); ok {
		for _, b := range body.Blocks {
			children = append(children, NewHCLBlock(b.AsHCLBlock(), ctx, moduleBlock))
		}

		return &Block{
			context:     ctx,
			hclBlock:    hclBlock,
			moduleBlock: moduleBlock,
			childBlocks: children,
		}
	}

	content, _, diag := hclBlock.Body.PartialContent(terraformSchemaV012)
	if diag != nil && diag.HasErrors() {
		log.Debugf("error loading partial content from hcl file %s", diag.Error())

		return &Block{
			context:     ctx,
			hclBlock:    hclBlock,
			moduleBlock: moduleBlock,
			childBlocks: children,
		}
	}

	for _, hb := range content.Blocks {
		children = append(children, NewHCLBlock(hb, ctx, moduleBlock))
	}

	return &Block{
		context:     ctx,
		hclBlock:    hclBlock,
		moduleBlock: moduleBlock,
		childBlocks: children,
	}
}

func (b *Block) InjectBlock(block *Block, name string) {
	block.hclBlock.Labels = []string{}
	block.hclBlock.Type = name
	for attrName, attr := range block.Attributes() {
		b.context.Root().SetByDot(attr.Value(), fmt.Sprintf("%s.%s.%s", b.Reference().String(), name, attrName))
	}
	b.childBlocks = append(b.childBlocks, block)
}

func (b *Block) IsCountExpanded() bool {
	return b.expanded
}

func (b *Block) Clone(index cty.Value) *Block {
	var childCtx *Context
	if b.context != nil {
		childCtx = b.context.NewChild()
	} else {
		childCtx = NewContext(&hcl.EvalContext{}, nil)
	}

	cloneHCL := *b.hclBlock

	clone := NewHCLBlock(&cloneHCL, childCtx, b.moduleBlock)
	if len(clone.hclBlock.Labels) > 0 {
		position := len(clone.hclBlock.Labels) - 1
		labels := make([]string, len(clone.hclBlock.Labels))
		for i := 0; i < len(labels); i++ {
			labels[i] = clone.hclBlock.Labels[i]
		}
		if index.IsKnown() && !index.IsNull() {
			switch index.Type() {
			case cty.Number:
				f, _ := index.AsBigFloat().Float64()
				labels[position] = fmt.Sprintf("%s[%d]", clone.hclBlock.Labels[position], int(f))
			case cty.String:
				labels[position] = fmt.Sprintf("%s[%q]", clone.hclBlock.Labels[position], index.AsString())
			default:
				log.Debugf("Invalid key type in iterable: %#v", index.Type())
				labels[position] = fmt.Sprintf("%s[%#v]", clone.hclBlock.Labels[position], index)
			}
		} else {
			labels[position] = fmt.Sprintf("%s[%d]", clone.hclBlock.Labels[position], b.cloneIndex)
		}
		clone.hclBlock.Labels = labels
	}
	indexVal, _ := gocty.ToCtyValue(index, cty.Number)
	clone.context.SetByDot(indexVal, "count.index")
	clone.expanded = true
	b.cloneIndex++

	return clone
}

func (b *Block) Context() *Context {
	return b.context
}

func (b *Block) OverrideContext(ctx *Context) {
	b.context = ctx
	for _, block := range b.childBlocks {
		block.OverrideContext(ctx.NewChild())
	}
}

func (b *Block) HasModuleBlock() bool {
	if b == nil {
		return false
	}
	return b.moduleBlock != nil
}

func (b *Block) Type() string {
	return b.hclBlock.Type
}

func (b *Block) Labels() []string {
	return b.hclBlock.Labels
}

func (b *Block) getHCLAttributes() hcl.Attributes {
	switch body := b.hclBlock.Body.(type) {
	case *hclsyntax.Body:
		attributes := make(hcl.Attributes)
		for _, a := range body.Attributes {
			attributes[a.Name] = a.AsHCLAttribute()
		}
		return attributes
	default:
		_, body, diag := b.hclBlock.Body.PartialContent(terraformSchemaV012)
		if diag != nil {
			return nil
		}
		attrs, diag := body.JustAttributes()
		if diag != nil {
			return nil
		}
		return attrs
	}
}

func (b *Block) GetBlock(name string) *Block {
	var returnBlock *Block
	if b == nil || b.hclBlock == nil {
		return returnBlock
	}
	for _, child := range b.childBlocks {
		if child.Type() == name {
			return child
		}
	}
	return returnBlock
}

func (b *Block) AllBlocks() Blocks {
	if b == nil || b.hclBlock == nil {
		return nil
	}
	return b.childBlocks
}

func (b *Block) GetAttributes() []*Attribute {
	var results []*Attribute
	if b == nil || b.hclBlock == nil {
		return nil
	}

	for _, attr := range b.getHCLAttributes() {
		results = append(results, &Attribute{HCLAttr: attr, Ctx: b.context})
	}

	b.cachedAttributes = results
	return results
}

func (b *Block) GetAttribute(name string) *Attribute {
	var attr *Attribute
	if b == nil || b.hclBlock == nil {
		return attr
	}
	for _, attr := range b.GetAttributes() {
		if attr.Name() == name {
			return attr
		}
	}
	return attr
}

func (b *Block) Reference() *Reference {
	var parts []string
	if b.Type() != "resource" {
		parts = append(parts, b.Type())
	}

	parts = append(parts, b.Labels()...)
	ref, _ := newReference(parts)
	return ref
}

// LocalName is the name relative to the current module
func (b *Block) LocalName() string {
	return b.Reference().String()
}

func (b *Block) FullName() string {
	if b.moduleBlock != nil {
		return fmt.Sprintf(
			"%s.%s",
			b.moduleBlock.FullName(),
			b.LocalName(),
		)
	}

	return b.LocalName()
}

func (b *Block) TypeLabel() string {
	if len(b.Labels()) > 0 {
		return b.Labels()[0]
	}
	return ""
}

func (b *Block) NameLabel() string {
	if len(b.Labels()) > 1 {
		return b.Labels()[1]
	}
	return ""
}

func (b *Block) HasChild(childElement string) bool {
	return b.GetAttribute(childElement).IsNotNil() || b.GetBlock(childElement) != nil
}

func (b *Block) Label() string {
	return strings.Join(b.hclBlock.Labels, ".")
}

func (b *Block) Attributes() map[string]*Attribute {
	attributes := make(map[string]*Attribute)

	for _, attr := range b.GetAttributes() {
		attributes[attr.Name()] = attr
	}

	return attributes
}

func (b *Block) Values() cty.Value {
	values := make(map[string]cty.Value)

	for _, attribute := range b.GetAttributes() {
		values[attribute.Name()] = attribute.Value()
	}

	return cty.ObjectVal(values)
}

func loadBlocksFromFile(file *hcl.File) (hcl.Blocks, error) {
	contents, diags := file.Body.Content(terraformSchemaV012)
	if diags != nil && diags.HasErrors() {
		return nil, diags
	}

	if contents == nil {
		return nil, fmt.Errorf("file contents is empty")
	}

	return contents.Blocks, nil
}
