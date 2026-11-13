package app

import "core/cli/app/internal/projectbinding"

func (m *sessionPickerModel) ensureSelectedVisible(tab *sessionPickerTab) {
	selected := tab.selectedIndex()
	if selected == nil {
		tab.offset = 0
		return
	}
	tab.offset = projectbinding.EnsureCursorVisible(
		*selected,
		tab.offset,
		m.visibleRowsRequest(tab),
	)
}

func (m *sessionPickerModel) visibleRowsFromOffset(tab *sessionPickerTab, offset int) []projectbinding.VisibleRow {
	request := m.visibleRowsRequest(tab)
	request.Offset = offset
	return projectbinding.VisibleRows(request)
}

func (m *sessionPickerModel) visibleRowsRequest(tab *sessionPickerTab) projectbinding.VisibleRowsRequest {
	return projectbinding.VisibleRowsRequest{
		ItemCount:  tab.itemCount(),
		LineBudget: m.visibleLineBudget(),
		HasPreview: func(index int) bool {
			return m.hasPreview(tab, index)
		},
	}
}
