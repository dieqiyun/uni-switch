"""Prepare local generated artwork for the application; no network or credentials."""
from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent


def extract_mark(source: Path) -> Image.Image:
    # Black-on-white generation: turn white coverage into alpha, remove tiny
    # off-white noise, and keep antialiased edges. The mark itself is pure black.
    image = Image.open(source).convert("L")
    alpha = image.point(lambda value: round(max(0, 243 - value) * 255 / 243))
    mark = Image.new("RGBA", image.size, (0, 0, 0, 0))
    mark.putalpha(alpha)
    bounds = alpha.getbbox()
    if not bounds:
        raise ValueError("No monochrome artwork found")
    return mark.crop(bounds)


def square(mark: Image.Image, size: int, inset: float = 0.08) -> Image.Image:
    canvas = Image.new("RGBA", (size, size))
    limit = round(size * (1 - inset * 2))
    scaled = mark.copy()
    scaled.thumbnail((limit, limit), Image.Resampling.LANCZOS)
    canvas.alpha_composite(scaled, ((size - scaled.width) // 2, (size - scaled.height) // 2))
    return canvas


def main():
    output = ROOT / "output/imagegen"
    assets = ROOT / "src/assets"
    assets.mkdir(exist_ok=True)
    mark = extract_mark(output / "uni-switch-logo-monochrome-generated.png")
    master = square(mark, 1024)
    master.save(output / "uni-switch-logo-monochrome-transparent.png")
    master.resize((256, 256), Image.Resampling.LANCZOS).save(assets / "uni-switch-logo.png")
    # A white tile keeps black artwork visible on both light and dark taskbars.
    # Only the native icon has a tile; the sidebar uses the transparent mark.
    icon = Image.new("RGBA", (1024, 1024))
    ImageDraw.Draw(icon).rounded_rectangle((16, 16, 1007, 1007), radius=190, fill="white")
    icon.alpha_composite(square(mark, 1024, inset=0.16))
    icon.save(output / "uni-switch-icon-monochrome.png")
    icon.resize((32, 32), Image.Resampling.LANCZOS).save(ROOT / "public/favicon.png")
    preview = Image.new("RGBA", (400, 400), "white")
    preview.alpha_composite(square(mark, 400, inset=0.2))
    preview.convert("RGB").save(output / "uni-switch-logo-monochrome-preview.png")
    print("Prepared black uni-switch mark, white-tile native icon and favicon.")


if __name__ == "__main__":
    main()
