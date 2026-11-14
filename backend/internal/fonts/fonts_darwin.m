//go:build darwin && cgo && !nogui

#import <AppKit/AppKit.h>
#import <CoreText/CoreText.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

static NSString *fontText(CFTypeRef value) {
    NSString *text = value && CFGetTypeID(value) == CFStringGetTypeID()
        ? [(__bridge NSString *)value copy] : nil;
    if (value) CFRelease(value);
    return [text autorelease];
}

static NSString *fontName(CTFontDescriptorRef descriptor, CFStringRef key) {
    return fontText(CTFontDescriptorCopyAttribute(descriptor, key));
}

static double axis(CFDictionaryRef variation, uint32_t tag, double fallback) {
    if (!variation) return fallback;
    int64_t number = tag;
    CFNumberRef key = CFNumberCreate(NULL, kCFNumberSInt64Type, &number);
    CFNumberRef value = CFDictionaryGetValue(variation, key);
    double out = fallback;
    if (value && CFGetTypeID(value) == CFNumberGetTypeID()) CFNumberGetValue(value, kCFNumberDoubleType, &out);
    CFRelease(key);
    return out;
}

// Apple AAT fonts need not have an OS/2 table. Their native trait uses
// AppKit's weight scale, not the OpenType/CSS numeric scale.
static int traitWeight(CTFontRef font) {
    const CGFloat native[] = {NSFontWeightUltraLight, NSFontWeightThin, NSFontWeightLight,
        NSFontWeightRegular, NSFontWeightMedium, NSFontWeightSemibold,
        NSFontWeightBold, NSFontWeightHeavy, NSFontWeightBlack};
    CFDictionaryRef traits = CTFontCopyTraits(font);
    CFNumberRef value = traits ? CFDictionaryGetValue(traits, kCTFontWeightTrait) : NULL;
    double weight = 0;
    if (value) CFNumberGetValue(value, kCFNumberDoubleType, &weight);
    if (traits) CFRelease(traits);
    int best = 3;
    for (int i = 0; i < 9; ++i) if (fabs(weight - native[i]) < fabs(weight - native[best])) best = i;
    return (best + 1) * 100;
}

char *uniSwitchInstalledFonts(void) {
    @autoreleasepool {
        CTFontCollectionRef collection = CTFontCollectionCreateFromAvailableFonts(NULL);
        if (!collection) return NULL;
        CFArrayRef descriptors = CTFontCollectionCreateMatchingFontDescriptors(collection);
        CFRelease(collection);
        if (!descriptors) return NULL;
        NSMutableArray *faces = [NSMutableArray array];
        const double widths[] = {100, 50, 62.5, 75, 87.5, 100, 112.5, 125, 150, 200};
        for (CFIndex i = 0; i < CFArrayGetCount(descriptors); ++i) {
            CTFontDescriptorRef descriptor = (CTFontDescriptorRef)CFArrayGetValueAtIndex(descriptors, i);
            CFTypeRef downloadable = CTFontDescriptorCopyAttribute(descriptor, kCTFontDownloadableAttribute);
            CFTypeRef downloaded = CTFontDescriptorCopyAttribute(descriptor, kCTFontDownloadedAttribute);
            // A bundled font was never downloaded either. Only a font
            // explicitly offered for download can be a remote-only face.
            BOOL remote = downloadable && CFEqual(downloadable, kCFBooleanTrue)
                && downloaded && CFEqual(downloaded, kCFBooleanFalse);
            if (downloadable) CFRelease(downloadable);
            if (downloaded) CFRelease(downloaded);
            if (remote) continue;
            CTFontRef font = CTFontCreateWithFontDescriptor(descriptor, 0, NULL);
            if (!font) continue;
            NSString *family = fontName(descriptor, kCTFontFamilyNameAttribute);
            NSString *name = fontName(descriptor, kCTFontStyleNameAttribute);
            if (!family.length) family = fontText(CTFontCopyFamilyName(font));
            if (!name.length) name = fontText(CTFontCopyName(font, kCTFontSubFamilyNameKey));
            // Dot-prefixed faces are private system UI implementations, not
            // families the webview can request by name.
            if (!family.length || !name.length || [family hasPrefix:@"."]) { CFRelease(font); continue; }
            CTFontSymbolicTraits traits = CTFontGetSymbolicTraits(font);
            double weight = traitWeight(font);
            double stretch = traits & kCTFontCondensedTrait ? 75 : traits & kCTFontExpandedTrait ? 125 : 100;
            NSString *slope = traits & kCTFontItalicTrait ? @"italic" : @"normal";
            // OpenType weights are CSS weights; CoreText's floating trait
            // scale is not. Keep the original table values where available.
            CFDataRef table = CTFontCopyTable(font, kCTFontTableOS2, kCTFontTableOptionNoOptions);
            if (table) {
                const UInt8 *p = CFDataGetBytePtr(table);
                if (CFDataGetLength(table) >= 8) {
                    int w = (p[4] << 8) | p[5], width = (p[6] << 8) | p[7];
                    if (w > 0 && w <= 1000) weight = w;
                    if (width > 0 && width <= 9) stretch = widths[width];
                }
                if (CFDataGetLength(table) >= 64 && (p[62] & 2)) slope = @"oblique";
                CFRelease(table);
            }
            // Named variable instances can override the base font's table.
            CFDictionaryRef variation = CTFontCopyVariation(font);
            weight = axis(variation, 'wght', weight);
            stretch = axis(variation, 'wdth', stretch);
            if (axis(variation, 'ital', 0) != 0) slope = @"italic";
            else if (axis(variation, 'slnt', 0) != 0) slope = @"oblique";
            if (variation) CFRelease(variation);
            CFRelease(font);
            [faces addObject:@{@"family": family, @"name": name, @"weight": @(weight), @"style": slope, @"stretch": @(stretch)}];
        }
        CFRelease(descriptors);
        NSData *json = [NSJSONSerialization dataWithJSONObject:faces options:0 error:NULL];
        if (!json) return NULL;
        char *out = malloc(json.length + 1);
        if (!out) return NULL;
        memcpy(out, json.bytes, json.length);
        out[json.length] = 0;
        return out;
    }
}
