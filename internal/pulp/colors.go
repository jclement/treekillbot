// The CSS named colours, plus the greyscale helpers this tool leans on.
//
// Named colours exist for convenience when sketching. The shipped themes do not
// use them: a form destined for a laser printer is authored in gray() so that
// the tint written is the tint the printer receives (see DESIGN.md D10). These
// are RGB, and an RGB grey is not the same thing as a DeviceGray grey once a
// RIP gets hold of it.
package pulp

import (
	"sort"

	"github.com/jclement/treekillbot/internal/paint"
)

// namedColors maps the 148 CSS colour keywords to their sRGB values.
var namedColors = map[string]paint.Color{
	"aliceblue":            paint.RGB8(0xf0, 0xf8, 0xff),
	"antiquewhite":         paint.RGB8(0xfa, 0xeb, 0xd7),
	"aqua":                 paint.RGB8(0x00, 0xff, 0xff),
	"aquamarine":           paint.RGB8(0x7f, 0xff, 0xd4),
	"azure":                paint.RGB8(0xf0, 0xff, 0xff),
	"beige":                paint.RGB8(0xf5, 0xf5, 0xdc),
	"bisque":               paint.RGB8(0xff, 0xe4, 0xc4),
	"black":                paint.RGB8(0x00, 0x00, 0x00),
	"blanchedalmond":       paint.RGB8(0xff, 0xeb, 0xcd),
	"blue":                 paint.RGB8(0x00, 0x00, 0xff),
	"blueviolet":           paint.RGB8(0x8a, 0x2b, 0xe2),
	"brown":                paint.RGB8(0xa5, 0x2a, 0x2a),
	"burlywood":            paint.RGB8(0xde, 0xb8, 0x87),
	"cadetblue":            paint.RGB8(0x5f, 0x9e, 0xa0),
	"chartreuse":           paint.RGB8(0x7f, 0xff, 0x00),
	"chocolate":            paint.RGB8(0xd2, 0x69, 0x1e),
	"coral":                paint.RGB8(0xff, 0x7f, 0x50),
	"cornflowerblue":       paint.RGB8(0x64, 0x95, 0xed),
	"cornsilk":             paint.RGB8(0xff, 0xf8, 0xdc),
	"crimson":              paint.RGB8(0xdc, 0x14, 0x3c),
	"cyan":                 paint.RGB8(0x00, 0xff, 0xff),
	"darkblue":             paint.RGB8(0x00, 0x00, 0x8b),
	"darkcyan":             paint.RGB8(0x00, 0x8b, 0x8b),
	"darkgoldenrod":        paint.RGB8(0xb8, 0x86, 0x0b),
	"darkgray":             paint.RGB8(0xa9, 0xa9, 0xa9),
	"darkgreen":            paint.RGB8(0x00, 0x64, 0x00),
	"darkgrey":             paint.RGB8(0xa9, 0xa9, 0xa9),
	"darkkhaki":            paint.RGB8(0xbd, 0xb7, 0x6b),
	"darkmagenta":          paint.RGB8(0x8b, 0x00, 0x8b),
	"darkolivegreen":       paint.RGB8(0x55, 0x6b, 0x2f),
	"darkorange":           paint.RGB8(0xff, 0x8c, 0x00),
	"darkorchid":           paint.RGB8(0x99, 0x32, 0xcc),
	"darkred":              paint.RGB8(0x8b, 0x00, 0x00),
	"darksalmon":           paint.RGB8(0xe9, 0x96, 0x7a),
	"darkseagreen":         paint.RGB8(0x8f, 0xbc, 0x8f),
	"darkslateblue":        paint.RGB8(0x48, 0x3d, 0x8b),
	"darkslategray":        paint.RGB8(0x2f, 0x4f, 0x4f),
	"darkslategrey":        paint.RGB8(0x2f, 0x4f, 0x4f),
	"darkturquoise":        paint.RGB8(0x00, 0xce, 0xd1),
	"darkviolet":           paint.RGB8(0x94, 0x00, 0xd3),
	"deeppink":             paint.RGB8(0xff, 0x14, 0x93),
	"deepskyblue":          paint.RGB8(0x00, 0xbf, 0xff),
	"dimgray":              paint.RGB8(0x69, 0x69, 0x69),
	"dimgrey":              paint.RGB8(0x69, 0x69, 0x69),
	"dodgerblue":           paint.RGB8(0x1e, 0x90, 0xff),
	"firebrick":            paint.RGB8(0xb2, 0x22, 0x22),
	"floralwhite":          paint.RGB8(0xff, 0xfa, 0xf0),
	"forestgreen":          paint.RGB8(0x22, 0x8b, 0x22),
	"fuchsia":              paint.RGB8(0xff, 0x00, 0xff),
	"gainsboro":            paint.RGB8(0xdc, 0xdc, 0xdc),
	"ghostwhite":           paint.RGB8(0xf8, 0xf8, 0xff),
	"gold":                 paint.RGB8(0xff, 0xd7, 0x00),
	"goldenrod":            paint.RGB8(0xda, 0xa5, 0x20),
	"gray":                 paint.RGB8(0x80, 0x80, 0x80),
	"green":                paint.RGB8(0x00, 0x80, 0x00),
	"greenyellow":          paint.RGB8(0xad, 0xff, 0x2f),
	"grey":                 paint.RGB8(0x80, 0x80, 0x80),
	"honeydew":             paint.RGB8(0xf0, 0xff, 0xf0),
	"hotpink":              paint.RGB8(0xff, 0x69, 0xb4),
	"indianred":            paint.RGB8(0xcd, 0x5c, 0x5c),
	"indigo":               paint.RGB8(0x4b, 0x00, 0x82),
	"ivory":                paint.RGB8(0xff, 0xff, 0xf0),
	"khaki":                paint.RGB8(0xf0, 0xe6, 0x8c),
	"lavender":             paint.RGB8(0xe6, 0xe6, 0xfa),
	"lavenderblush":        paint.RGB8(0xff, 0xf0, 0xf5),
	"lawngreen":            paint.RGB8(0x7c, 0xfc, 0x00),
	"lemonchiffon":         paint.RGB8(0xff, 0xfa, 0xcd),
	"lightblue":            paint.RGB8(0xad, 0xd8, 0xe6),
	"lightcoral":           paint.RGB8(0xf0, 0x80, 0x80),
	"lightcyan":            paint.RGB8(0xe0, 0xff, 0xff),
	"lightgoldenrodyellow": paint.RGB8(0xfa, 0xfa, 0xd2),
	"lightgray":            paint.RGB8(0xd3, 0xd3, 0xd3),
	"lightgreen":           paint.RGB8(0x90, 0xee, 0x90),
	"lightgrey":            paint.RGB8(0xd3, 0xd3, 0xd3),
	"lightpink":            paint.RGB8(0xff, 0xb6, 0xc1),
	"lightsalmon":          paint.RGB8(0xff, 0xa0, 0x7a),
	"lightseagreen":        paint.RGB8(0x20, 0xb2, 0xaa),
	"lightskyblue":         paint.RGB8(0x87, 0xce, 0xfa),
	"lightslategray":       paint.RGB8(0x77, 0x88, 0x99),
	"lightslategrey":       paint.RGB8(0x77, 0x88, 0x99),
	"lightsteelblue":       paint.RGB8(0xb0, 0xc4, 0xde),
	"lightyellow":          paint.RGB8(0xff, 0xff, 0xe0),
	"lime":                 paint.RGB8(0x00, 0xff, 0x00),
	"limegreen":            paint.RGB8(0x32, 0xcd, 0x32),
	"linen":                paint.RGB8(0xfa, 0xf0, 0xe6),
	"magenta":              paint.RGB8(0xff, 0x00, 0xff),
	"maroon":               paint.RGB8(0x80, 0x00, 0x00),
	"mediumaquamarine":     paint.RGB8(0x66, 0xcd, 0xaa),
	"mediumblue":           paint.RGB8(0x00, 0x00, 0xcd),
	"mediumorchid":         paint.RGB8(0xba, 0x55, 0xd3),
	"mediumpurple":         paint.RGB8(0x93, 0x70, 0xdb),
	"mediumseagreen":       paint.RGB8(0x3c, 0xb3, 0x71),
	"mediumslateblue":      paint.RGB8(0x7b, 0x68, 0xee),
	"mediumspringgreen":    paint.RGB8(0x00, 0xfa, 0x9a),
	"mediumturquoise":      paint.RGB8(0x48, 0xd1, 0xcc),
	"mediumvioletred":      paint.RGB8(0xc7, 0x15, 0x85),
	"midnightblue":         paint.RGB8(0x19, 0x19, 0x70),
	"mintcream":            paint.RGB8(0xf5, 0xff, 0xfa),
	"mistyrose":            paint.RGB8(0xff, 0xe4, 0xe1),
	"moccasin":             paint.RGB8(0xff, 0xe4, 0xb5),
	"navajowhite":          paint.RGB8(0xff, 0xde, 0xad),
	"navy":                 paint.RGB8(0x00, 0x00, 0x80),
	"oldlace":              paint.RGB8(0xfd, 0xf5, 0xe6),
	"olive":                paint.RGB8(0x80, 0x80, 0x00),
	"olivedrab":            paint.RGB8(0x6b, 0x8e, 0x23),
	"orange":               paint.RGB8(0xff, 0xa5, 0x00),
	"orangered":            paint.RGB8(0xff, 0x45, 0x00),
	"orchid":               paint.RGB8(0xda, 0x70, 0xd6),
	"palegoldenrod":        paint.RGB8(0xee, 0xe8, 0xaa),
	"palegreen":            paint.RGB8(0x98, 0xfb, 0x98),
	"paleturquoise":        paint.RGB8(0xaf, 0xee, 0xee),
	"palevioletred":        paint.RGB8(0xdb, 0x70, 0x93),
	"papayawhip":           paint.RGB8(0xff, 0xef, 0xd5),
	"peachpuff":            paint.RGB8(0xff, 0xda, 0xb9),
	"peru":                 paint.RGB8(0xcd, 0x85, 0x3f),
	"pink":                 paint.RGB8(0xff, 0xc0, 0xcb),
	"plum":                 paint.RGB8(0xdd, 0xa0, 0xdd),
	"powderblue":           paint.RGB8(0xb0, 0xe0, 0xe6),
	"purple":               paint.RGB8(0x80, 0x00, 0x80),
	"rebeccapurple":        paint.RGB8(0x66, 0x33, 0x99),
	"red":                  paint.RGB8(0xff, 0x00, 0x00),
	"rosybrown":            paint.RGB8(0xbc, 0x8f, 0x8f),
	"royalblue":            paint.RGB8(0x41, 0x69, 0xe1),
	"saddlebrown":          paint.RGB8(0x8b, 0x45, 0x13),
	"salmon":               paint.RGB8(0xfa, 0x80, 0x72),
	"sandybrown":           paint.RGB8(0xf4, 0xa4, 0x60),
	"seagreen":             paint.RGB8(0x2e, 0x8b, 0x57),
	"seashell":             paint.RGB8(0xff, 0xf5, 0xee),
	"sienna":               paint.RGB8(0xa0, 0x52, 0x2d),
	"silver":               paint.RGB8(0xc0, 0xc0, 0xc0),
	"skyblue":              paint.RGB8(0x87, 0xce, 0xeb),
	"slateblue":            paint.RGB8(0x6a, 0x5a, 0xcd),
	"slategray":            paint.RGB8(0x70, 0x80, 0x90),
	"slategrey":            paint.RGB8(0x70, 0x80, 0x90),
	"snow":                 paint.RGB8(0xff, 0xfa, 0xfa),
	"springgreen":          paint.RGB8(0x00, 0xff, 0x7f),
	"steelblue":            paint.RGB8(0x46, 0x82, 0xb4),
	"tan":                  paint.RGB8(0xd2, 0xb4, 0x8c),
	"teal":                 paint.RGB8(0x00, 0x80, 0x80),
	"thistle":              paint.RGB8(0xd8, 0xbf, 0xd8),
	"tomato":               paint.RGB8(0xff, 0x63, 0x47),
	"turquoise":            paint.RGB8(0x40, 0xe0, 0xd0),
	"violet":               paint.RGB8(0xee, 0x82, 0xee),
	"wheat":                paint.RGB8(0xf5, 0xde, 0xb3),
	"white":                paint.RGB8(0xff, 0xff, 0xff),
	"whitesmoke":           paint.RGB8(0xf5, 0xf5, 0xf5),
	"yellow":               paint.RGB8(0xff, 0xff, 0x00),
	"yellowgreen":          paint.RGB8(0x9a, 0xcd, 0x32),
}

// NamedColor looks up a CSS colour keyword. The name must already be
// lowercased by the caller.
//
// "transparent" is handled here rather than in the table because it is not an
// RGB value at all: it is zero alpha, and a node with a transparent background
// paints nothing, which is different from painting white.
func NamedColor(name string) (paint.Color, bool) {
	if name == "transparent" {
		return paint.Transparent, true
	}
	c, ok := namedColors[name]
	return c, ok
}

// NamedColorNames returns every keyword in sorted order, for the did-you-mean
// suggestions and for `treekillbot docs colors`. Sorted because map order is
// not allowed to reach the user (see DESIGN.md section 4).
func NamedColorNames() []string {
	out := make([]string, 0, len(namedColors))
	for name := range namedColors {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
