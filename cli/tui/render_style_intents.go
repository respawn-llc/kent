package tui

type StyleIntent uint16

const (
	ThemeForeground StyleIntent = 1 << iota
	Subdued
	PrimaryForeground
	SuccessForeground
	WarningForeground
	ErrorForeground
	Faint
	ShellPreview
	SyntaxHighlighted
	DiffAdded
	DiffRemoved
)

func (intent StyleIntent) Has(flag StyleIntent) bool {
	return intent&flag != 0
}

type transcriptRenderWrapMode uint8

const (
	transcriptRenderWrapModeViewport transcriptRenderWrapMode = iota
	transcriptRenderWrapModePreserved
)

type transcriptRenderContent struct {
	Lines    []transcriptRenderLine
	WrapMode transcriptRenderWrapMode
}

type transcriptRenderLine struct {
	Text    string
	Intents StyleIntent
}

type transcriptLayoutLine struct {
	Prefix         string
	Text           string
	Intents        StyleIntent
	ShowRoleSymbol bool
}

type ansiIntentPalette struct {
	ThemeForeground   rgbColor
	SubduedForeground rgbColor
	PrimaryForeground rgbColor
	SuccessForeground rgbColor
	WarningForeground rgbColor
	ErrorForeground   rgbColor
}

func applyANSIStyleIntents(text string, palette ansiIntentPalette, intents StyleIntent) string {
	if text == "" {
		return text
	}
	transform := ansiStyleTransform{}
	switch {
	case intents.Has(Subdued):
		transform.DefaultForeground = &palette.SubduedForeground
		transform.ForceFaint = true
	case intents.Has(PrimaryForeground):
		transform.DefaultForeground = &palette.PrimaryForeground
	case intents.Has(SuccessForeground):
		transform.DefaultForeground = &palette.SuccessForeground
	case intents.Has(WarningForeground):
		transform.DefaultForeground = &palette.WarningForeground
	case intents.Has(ErrorForeground):
		transform.DefaultForeground = &palette.ErrorForeground
	case intents.Has(ThemeForeground):
		transform.DefaultForeground = &palette.ThemeForeground
	default:
		if !intents.Has(Faint) {
			return text
		}
	}
	if intents.Has(Subdued) || intents.Has(Faint) {
		transform.ForceFaint = true
	}
	return applyANSIStyleTransform(text, transform)
}

func themeANSIIntentPalette(theme string) ansiIntentPalette {
	return ansiIntentPalette{
		ThemeForeground:   themeForegroundColor(theme),
		SubduedForeground: themePreviewColor(theme),
		PrimaryForeground: themePrimaryColor(theme),
		SuccessForeground: themeSuccessColor(theme),
		WarningForeground: themeWarningColor(theme),
		ErrorForeground:   themeErrorColor(theme),
	}
}

func ApplyThemeStyleIntents(text, theme string, intents StyleIntent) string {
	return applyANSIStyleIntents(text, themeANSIIntentPalette(theme), intents)
}
