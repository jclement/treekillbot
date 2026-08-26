// Package taglines holds the one-liners printed under the banner, in the
// summary box and beside the version string.
//
// The list is a fixed table rather than anything generated, and selection is
// explicitly *not* seeded from the clock: everything else in treekillbot goes
// to some trouble to make the same input produce the same bytes, and a
// package-level rand seeded at init would put a source of run-to-run variance
// in a binary whose golden tests exist to prove there isn't one. Callers pass
// their own source, so a test can pin it and the CLI can seed from the wall
// clock at exactly one call site where that is a deliberate choice.
package taglines

import "math/rand/v2"

// taglines is the whole set. Nothing here may exceed maxLength or repeat
// another entry; taglines_test.go enforces both, and the count.
var taglines = []string{
	// Trees, and what becomes of them.
	"one tree at a time",
	"converting forests into good intentions",
	"arboreal consequences, elegant margins",
	"made of pixels, destined for pulp",
	"the forest gave, the printer took",
	"sustainably sourced, relentlessly consumed",
	"a tree died so your Tuesday could be ruled",
	"80gsm and no regrets",
	"paper: the original persistent storage",
	"bleached, pressed, and ruled to order",
	"from cellulose to calendar",
	"every spread has a stump behind it",
	"deforestation, but tastefully typeset",
	"trees are just stationery that hasn't happened yet",
	"a renewable resource, aggressively renewed",
	"pulped for your convenience",
	"the woodgrain was the first dot grid",
	"reams of it, and still not enough",
	"recycled paper, original excuses",
	"it grows back, mostly",

	// Printers, and their relationship with the truth.
	"the printer is not the problem, you are",
	"your printer is out of cyan again",
	"the printer knows what you did",
	"PC LOAD LETTER remains unexplained",
	"it printed fine yesterday",
	"now with 40% fewer paper jams",
	"the driver is always the last suspect",
	"a printer is a rumour with a power cable",
	"it says ready, it is not ready",
	"tray 2 is a state of mind",
	"print preview lies, but politely",
	"the print queue is a suggestion",
	"the printer will decide the margins",
	"duplex is a gamble, not a setting",
	"borderless printing, borderline results",
	"it wants magenta before it will print black",
	"wireless printing, wired anxiety",
	"the printer has opinions about A4",
	"offline again, standing right there",
	"it heard you and chose silence",

	// Ink and toner, priced accordingly.
	"toner sold separately",
	"ink is cheaper than therapy, barely",
	"priced per millilitre, like perfume",
	"the cartridge was half empty at purchase",
	"drum unit not included, obviously",
	"gray(0.85) is darker than you think",
	"dot gain is a personality trait",
	"hairlines under a quarter point are refused",
	"black is a colour your printer disputes",
	"the chip says empty, the tank disagrees",
	"third-party toner, first-class anxiety",
	"laser, because inkjets dry out judging you",
	"saving ink by shrinking your ambitions",
	"one long stripe down every page",
	"print in grayscale, save a small fortune",
	"cyan is always the first to go",
	"shake the cartridge, it works, somehow",
	"archival ink for disposable plans",
	"the fuser has a smell and a schedule",
	"streaks are just texture",

	// Paper planning: futile, and a comfort.
	"a planner you cannot doomscroll",
	"no notifications, only regret",
	"beautiful forms, questionable follow-through",
	"print it, then ignore it on paper",
	"week one was immaculate",
	"the system works if you do, so, no",
	"blank boxes are a kind of optimism",
	"a spread a week keeps the panic away",
	"your habits, ruled at 5mm",
	"abandoned in February, beautifully",
	"hope, arranged in seven columns",
	"tracking the habits you already quit",
	"the grid does not judge, it just waits",
	"an empty page is a fresh start or a threat",
	"plans age badly, paper ages well",
	"Sunday is for pretending",
	"rewrite the same list until it is true",
	"a to-do list that cannot follow you home",
	"it will not sync, and that is the point",
	"crossing it out is the whole reward",

	// Paper against the alternative.
	"paper never asks you to sign in",
	"no cloud, no sync, no excuses",
	"an offline-first planner, aggressively",
	"zero telemetry, one printer",
	"your data stays in the drawer",
	"no subscription, no store, no update nag",
	"the battery lasts a lifetime",
	"works in airplane mode and in an actual plane",
	"no dark mode, only lights off",
	"it cannot be discontinued by a startup",
	"no notebook has ever been rate limited",
	"paper does not have a roadmap",
	"the original local-first storage",
	"no login screen, just a cover",
	"immune to outages, vulnerable to coffee",
	"encrypted end to end by your handwriting",
	"your notebook will not be acquired",
	"terms of service: do not lose it",
	"free forever, aside from the toner",
	"the only format with a 500-year track record",

	// PDFs, and being the same twice.
	"PDF: the last honest file format",
	"byte identical, run after run",
	"deterministic output, indeterminate outcomes",
	"same input, same bytes, every time",
	"reproducible builds, irreproducible weeks",
	"no wall clock in the metadata",
	"golden files all the way down",
	"integer ticks, sixteen to the point",
	"the children sum to the parent, exactly",
	"floats were considered and declined",
	"the margins are correct to the tick",
	"pixel perfect, or else it is a bug",
	"it fits or it errors, never both",
	"overflow is an error, not a shrug",
	"the layout engine has only arithmetic",
	"a document, not a webpage in a costume",
	"fonts embedded, no surprises at the printer",
	"what you see is what gets pulped",
	"the checksum is the contract",
	"no JavaScript in this PDF, ever",

	// The language.
	"indentation is the entire grammar",
	"it is not YAML, and that is deliberate",
	"repeated siblings, no dashes required",
	"a caret under the exact bad token",
	"did you mean line-style?",
	"errors in gcc format, for your editor",
	"seven lines for a whole week",
	"one language for themes and documents",
	"no calc(), no arithmetic, no regrets",
	"when: instead of if, on purpose",
	"the schema decides what is a property",
	"every token knows its byte span",
	"variables must be declared, always",
	"no ambient environment, no exfiltration",
	"strftime, because %Y cannot be a typo",
	"Q1 2006 review stays in 2006",
	"week 53 of the wrong year, solved",
	"one anchor date, one flag, one reality",
	"themes are more of the same language",
	"floats live at the parser and nowhere else",

	// Analogue nostalgia.
	"remember the smell of a fresh ream",
	"the Filofax was right all along",
	"mimeograph blue, now in DeviceGray",
	"dot matrix would have been louder",
	"tractor feed not supported, sadly",
	"carbon copy, no CC field",
	"the fax machine died for this",
	"a Rolodex you can print",
	"graph paper from the good years",
	"bullet journals without the influencers",
	"handwriting: the original biometric",
	"the pen is still the fastest input device",
	"legal pads were an entire aesthetic",
	"index cards, but computed",
	"Cornell notes, un-ironically",
	"day planners outlived the day planner era",
	"a three-ring binder is still a database",
	"hole punch alignment is a real science",
	"margins ruled in red, like school",
	"a spreadsheet you can fold",

	// The ritual of printing something out.
	"print, punch, forget",
	"the office printer is a shared trauma",
	"someone left forty pages in the tray",
	"collate is a verb and a prayer",
	"two sided, short edge, wrong again",
	"print to PDF, then print the PDF",
	"the shredder is downstream of all this",
	"laminate it if you really mean it",
	"a stapler is a build step",
	"filed under: never opened again",
	"print a spare, you always need a spare",
	"the copy room has seen things",
	"reams outlast roadmaps",
	"binder clips are load bearing",
	"it looks better on real paper, it always does",
	"test print on the back of a failed one",
	"save it as a template, print it forever",
	"one page, front and back, no more",
	"paper cuts are an occupational hazard",
	"the recycling bin is the real backlog",

	// Everything else.
	"turning text into stationery since today",
	"a compiler whose output is a forest",
	"build your week, literally",
	"the only build step that ends in paper",
	"CI cannot lint your handwriting",
	"compiles to cellulose",
	"we ship the forms, you ship the excuses",
	"no roadmap, just page sizes",
	"Letter and A4, forever at war",
	"the margins are non-negotiable",
	"it does one thing and jams once",
	"exit code 3 means your document is wrong",
	"exit code 4 means you asked for strict",
	"stdout is the artifact, stderr is the feelings",
	"pipe it to a file, or to a tree",
	"a static binary and some static paper",
	"no network access, by construction",
	"it will outlive the printer",
	"print it before the format changes",
	"the name was the honest part",
}

// Count is how many taglines there are. It exists so that a caller can size a
// Pick without allocating a copy of the whole slice.
const Count = 200

// maxLength is the longest a tagline may be. It is a hard cap rather than a
// guideline because these render into a fixed-width status line and a summary
// box, and a wrapped tagline reads as a bug.
const maxLength = 60

// All returns every tagline, in a stable order.
//
// The returned slice is a copy: this is the only export that hands out the
// whole table, and one caller sorting it in place would silently change what
// Pick returns for everybody else.
func All() []string {
	out := make([]string, len(taglines))
	copy(out, taglines)
	return out
}

// Pick returns the tagline at index n, wrapping so that any int is valid.
// Negative values wrap the same way, so callers can pass a raw hash without
// worrying about its sign.
//
// This is the deterministic entry point, and the one tests should use.
func Pick(n int) string {
	i := n % len(taglines)
	if i < 0 {
		i += len(taglines)
	}
	return taglines[i]
}

// Random returns a tagline chosen using the caller's source. Pass a seeded
// *rand.Rand for a reproducible sequence; the caller owns the decision to be
// unpredictable, because this package will not make it for them.
func Random(source *rand.Rand) string {
	return taglines[source.IntN(len(taglines))]
}
