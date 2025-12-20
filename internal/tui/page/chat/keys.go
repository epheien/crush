package chat

import (
	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	NewSession    key.Binding
	AddAttachment key.Binding
	Cancel        key.Binding
	Tab           key.Binding
	Details       key.Binding
	TogglePills   key.Binding
	PillLeft      key.Binding
	PillRight     key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "new session"),
		),
		AddAttachment: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "add attachment"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc", "alt+esc"),
			key.WithHelp("esc", "cancel"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "change focus"),
		),
		Details: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "toggle details"),
		),
		TogglePills: key.NewBinding(
			key.WithKeys("ctrl+space"),
			key.WithHelp("ctrl+space", "toggle tasks"),
		),
		PillLeft: key.NewBinding(
			key.WithKeys("left", "ctrl+b"),
			key.WithHelp("←/→ or ctrl+b/f", "switch section"),
		),
		PillRight: key.NewBinding(
			key.WithKeys("right", "ctrl+f"),
			key.WithHelp("←/→ or ctrl+b/f", "switch section"),
		),
	}
}
