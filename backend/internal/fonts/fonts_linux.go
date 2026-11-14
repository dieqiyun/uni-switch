//go:build linux && cgo && !nogui

package fonts

/*
#cgo pkg-config: fontconfig
#include <fontconfig/fontconfig.h>

static FcFontSet *uniSwitchFonts(void) {
    FcPattern *pattern = FcPatternCreate();
    if (!pattern) return NULL;
    FcPatternAddBool(pattern, FC_SCALABLE, FcTrue);
    FcObjectSet *objects = FcObjectSetBuild(FC_FAMILY, FC_STYLE, FC_WEIGHT, FC_SLANT, FC_WIDTH, NULL);
    if (!objects) { FcPatternDestroy(pattern); return NULL; }
    FcFontSet *fonts = FcFontList(NULL, pattern, objects);
    FcObjectSetDestroy(objects);
    FcPatternDestroy(pattern);
    return fonts;
}
static FcPattern *uniSwitchFontAt(FcFontSet *set, int i) { return set->fonts[i]; }
static const char *uniSwitchFontString(FcPattern *font, const char *key) {
    FcChar8 *value = NULL;
    return FcPatternGetString(font, key, 0, &value) == FcResultMatch ? (const char *)value : "";
}
static double uniSwitchFontNumber(FcPattern *font, const char *key, double fallback) {
    FcValue value;
    if (FcPatternGet(font, key, 0, &value) != FcResultMatch) return fallback;
    if (value.type == FcTypeInteger) return value.u.i;
    if (value.type == FcTypeDouble) return value.u.d;
    return fallback;
}
static const char *uniSwitchFontFamily(FcPattern *f) { return uniSwitchFontString(f, FC_FAMILY); }
static const char *uniSwitchFontStyle(FcPattern *f) { return uniSwitchFontString(f, FC_STYLE); }
static double uniSwitchFontWeight(FcPattern *f) { return uniSwitchFontNumber(f, FC_WEIGHT, -1); }
static double uniSwitchFontSlant(FcPattern *f) { return uniSwitchFontNumber(f, FC_SLANT, FC_SLANT_ROMAN); }
static double uniSwitchFontWidth(FcPattern *f) { return uniSwitchFontNumber(f, FC_WIDTH, 100); }
*/
import "C"

import "errors"

const Available = true

func installed() ([]Face, error) {
	set := C.uniSwitchFonts()
	if set == nil {
		return nil, errors.New("Fontconfig could not list installed fonts")
	}
	defer C.FcFontSetDestroy(set)
	faces := make([]Face, 0, int(set.nfont))
	for i := C.int(0); i < set.nfont; i++ {
		font := C.uniSwitchFontAt(set, i)
		// A variable font's range is not a named instance. Its enumerated
		// named instances have numeric weights and are included normally.
		weight := C.uniSwitchFontWeight(font)
		if weight < 0 {
			continue
		}
		style := "normal"
		switch int(C.uniSwitchFontSlant(font)) {
		case C.FC_SLANT_ITALIC:
			style = "italic"
		case C.FC_SLANT_OBLIQUE:
			style = "oblique"
		}
		faces = append(faces, Face{
			Family: C.GoString(C.uniSwitchFontFamily(font)),
			Name:   C.GoString(C.uniSwitchFontStyle(font)),
			Weight: float64(C.FcWeightToOpenTypeDouble(weight)),
			Style:  style, Stretch: float64(C.uniSwitchFontWidth(font)),
		})
	}
	return faces, nil
}
