package ui

import "image"

type Layout struct {
	Container
	Toolbar *TextArea
	Cols    *Columns
}

func NewLayout(ui *UI) *Layout {
	layout := &Layout{}
	layout.Container.Painter = layout
	layout.UI = ui

	layout.Toolbar = NewTextArea()
	layout.Toolbar.Data = layout
	layout.Toolbar.DynamicY = true
	layout.Toolbar.Colors = &ToolbarColors

	layout.Cols = NewColumns(layout)

	layout.AddChilds(&layout.Toolbar.Container, &layout.Cols.Container)
	return layout
}
func (layout *Layout) CalcArea(area *image.Rectangle) {
	a := *area
	layout.Area = a
	layout.Toolbar.CalcArea(&a)
	a.Min.Y = layout.Toolbar.Area.Max.Y
	// separator
	a.Min.Y += SeparatorWidth
	// cols
	layout.Cols.CalcArea(&a)
}
func (layout *Layout) Paint() {
	layout.Toolbar.Paint()
	// separator
	r1 := layout.Area
	r1.Min.Y = layout.Toolbar.Area.Max.Y
	r1.Max.Y = r1.Min.Y + SeparatorWidth
	layout.FillRectangle(&r1, &SeparatorColor)

	layout.Cols.Paint()
}
