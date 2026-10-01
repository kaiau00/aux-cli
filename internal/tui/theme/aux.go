package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// AuxTheme implements the Theme interface with Aux brand colors.
// It provides both dark and light variants.
type AuxTheme struct {
	BaseTheme
}

// NewAuxTheme creates a new instance of the Aux theme.
func NewAuxTheme() *AuxTheme {
	// Aux amber color palette, desaturated into a muted, natural earth-tone
	// family. Primary/Secondary/Accent keep the amber identity; Error/
	// Warning/Success/Info are each given their own distinct hue (rust,
	// burnt orange, sage, dusty teal) instead of all clustering in the same
	// yellow band the brand color lives in -- previously Primary and
	// Warning were the literal same hex, and Success/Info were both yellows
	// indistinguishable from Primary.
	// Dark mode colors
	darkBackground := "#1a1512"
	darkCurrentLine := "#231d16"
	darkSelection := "#3a2f22"
	darkForeground := "#e8dcc8"
	darkComment := "#9c8768"
	darkPrimary := "#c98a3c"
	darkSecondary := "#c2a05e"
	darkAccent := "#a86b34"
	darkRed := "#b5533d"
	darkOrange := "#c2703a"
	darkGreen := "#8a9a5b"
	darkCyan := "#6f8f8a"
	darkYellow := "#d9b876"
	darkBorder := "#5a4526"

	// Light mode colors
	lightBackground := "#faf5e9"
	lightCurrentLine := "#f3ead4"
	lightSelection := "#e8d7ad"
	lightForeground := "#2f2412"
	lightComment := "#7a6544"
	lightPrimary := "#9c6427"
	lightSecondary := "#8f7233"
	lightAccent := "#7a4c22"
	lightRed := "#943f2c"
	lightOrange := "#93532a"
	lightGreen := "#5f6b3a"
	lightCyan := "#3f6360" // darkened for WCAG contrast against the light background
	lightYellow := "#5c4419"
	lightBorder := "#c9ab72"

	theme := &AuxTheme{}

	// Base colors
	theme.PrimaryColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.SecondaryColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.AccentColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}

	// Status colors
	theme.ErrorColor = lipgloss.AdaptiveColor{
		Dark:  darkRed,
		Light: lightRed,
	}
	theme.WarningColor = lipgloss.AdaptiveColor{
		Dark:  darkOrange,
		Light: lightOrange,
	}
	theme.SuccessColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.InfoColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}

	// Text colors
	theme.TextColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}
	theme.TextMutedColor = lipgloss.AdaptiveColor{
		Dark:  darkComment,
		Light: lightComment,
	}
	theme.TextEmphasizedColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}

	// Background colors
	theme.BackgroundColor = lipgloss.AdaptiveColor{
		Dark:  darkBackground,
		Light: lightBackground,
	}
	theme.BackgroundSecondaryColor = lipgloss.AdaptiveColor{
		Dark:  darkCurrentLine,
		Light: lightCurrentLine,
	}
	theme.BackgroundDarkerColor = lipgloss.AdaptiveColor{
		Dark:  "#0d0a06",
		Light: "#fffdf7",
	}

	// Border colors
	theme.BorderNormalColor = lipgloss.AdaptiveColor{
		Dark:  darkBorder,
		Light: lightBorder,
	}
	theme.BorderFocusedColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.BorderDimColor = lipgloss.AdaptiveColor{
		Dark:  darkSelection,
		Light: lightSelection,
	}

	// Diff view colors
	theme.DiffAddedColor = lipgloss.AdaptiveColor{
		Dark:  "#9ba86a",
		Light: "#5f6b3a",
	}
	theme.DiffRemovedColor = lipgloss.AdaptiveColor{
		Dark:  "#c2703a",
		Light: "#93532a",
	}
	theme.DiffContextColor = lipgloss.AdaptiveColor{
		Dark:  "#b8a684",
		Light: "#7c5a1d",
	}
	theme.DiffHunkHeaderColor = lipgloss.AdaptiveColor{
		Dark:  "#c2a05e",
		Light: "#7a4c22",
	}
	theme.DiffHighlightAddedColor = lipgloss.AdaptiveColor{
		Dark:  "#e8ecc9",
		Light: "#a9b57a",
	}
	theme.DiffHighlightRemovedColor = lipgloss.AdaptiveColor{
		Dark:  "#ecd3bb",
		Light: "#c48a5e",
	}
	theme.DiffAddedBgColor = lipgloss.AdaptiveColor{
		Dark:  "#242918",
		Light: "#eef0dd",
	}
	theme.DiffRemovedBgColor = lipgloss.AdaptiveColor{
		Dark:  "#2b2016",
		Light: "#f3e4d6",
	}
	theme.DiffContextBgColor = lipgloss.AdaptiveColor{
		Dark:  darkBackground,
		Light: lightBackground,
	}
	theme.DiffLineNumberColor = lipgloss.AdaptiveColor{
		Dark:  "#9c8768",
		Light: "#7a6544",
	}
	theme.DiffAddedLineNumberBgColor = lipgloss.AdaptiveColor{
		Dark:  "#232a18",
		Light: "#e2e5c9",
	}
	theme.DiffRemovedLineNumberBgColor = lipgloss.AdaptiveColor{
		Dark:  "#2a2015",
		Light: "#ecdcc9",
	}

	// Markdown colors
	theme.MarkdownTextColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}
	theme.MarkdownHeadingColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.MarkdownLinkColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownLinkTextColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownCodeColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.MarkdownBlockQuoteColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.MarkdownEmphColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.MarkdownStrongColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}
	theme.MarkdownHorizontalRuleColor = lipgloss.AdaptiveColor{
		Dark:  darkComment,
		Light: lightComment,
	}
	theme.MarkdownListItemColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownListEnumerationColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownImageColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownImageTextColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownCodeBlockColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}

	// Syntax highlighting colors
	theme.SyntaxCommentColor = lipgloss.AdaptiveColor{
		Dark:  darkComment,
		Light: lightComment,
	}
	theme.SyntaxKeywordColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.SyntaxFunctionColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.SyntaxVariableColor = lipgloss.AdaptiveColor{
		Dark:  darkRed,
		Light: lightRed,
	}
	theme.SyntaxStringColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.SyntaxNumberColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}
	theme.SyntaxTypeColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.SyntaxOperatorColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.SyntaxPunctuationColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}

	return theme
}

func init() {
	// Register the Aux theme with the theme manager
	RegisterTheme("aux", NewAuxTheme())
}
