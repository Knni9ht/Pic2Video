"""Regenerate the original fixed demo fixture (development only).

Requires Pillow and an ffmpeg executable. Neither is needed to run Pic2Video.
Usage: python scripts/generate_demo.py PATH_TO_FFMPEG
"""
import math
from pathlib import Path
import subprocess
import sys

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "internal" / "provider" / "demo.mp4"
OUTPUT.parent.mkdir(parents=True, exist_ok=True)
WIDTH, HEIGHT, FPS, SECONDS = 360, 640, 24, 5


def font(size):
    for path in ["C:/Windows/Fonts/arial.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"]:
        if Path(path).is_file():
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


encoder = subprocess.Popen([
    sys.argv[1], "-y", "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", f"{WIDTH}x{HEIGHT}",
    "-r", str(FPS), "-i", "-", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p",
    "-crf", "24", "-movflags", "+faststart", str(OUTPUT),
], stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)

for frame in range(FPS * SECONDS):
    t = frame / FPS
    image = Image.new("RGB", (WIDTH, HEIGHT), "#eff5f2")
    draw = ImageDraw.Draw(image)
    for y in range(HEIGHT):
        k = y / HEIGHT
        draw.line((0, y, WIDTH, y), fill=(int(244 - 17*k), int(249 - 14*k), int(245 - 17*k)))
    draw.text((28, 32), "PIC2VIDEO", fill="#173e33", font=font(19))
    draw.rounded_rectangle((245, 27, 332, 57), radius=15, fill="#173e33")
    draw.text((262, 33), "DEMO", fill="white", font=font(15))
    bob = int(8 * math.sin(t * math.pi / 2.5))
    draw.ellipse((72, 390, 290, 428), fill="#d3dfd9")
    draw.rounded_rectangle((108, 173 + bob, 252, 400 + bob), radius=25, fill="#225744")
    draw.rounded_rectangle((143, 150 + bob, 217, 191 + bob), radius=8, fill="#123b2f")
    draw.rounded_rectangle((121, 229 + bob, 239, 345 + bob), radius=5, fill="#f3ead5")
    draw.text((138, 248 + bob), "STUDIO", fill="#225744", font=font(20))
    draw.line((142, 281 + bob, 218, 281 + bob), fill="#9aae9f", width=1)
    draw.text((139, 294 + bob), "SAMPLE", fill="#225744", font=font(18))
    draw.text((28, 470), "A little motion.", fill="#173e33", font=font(31))
    draw.text((28, 514), "A clear first look.", fill="#5c776b", font=font(21))
    draw.line((28, 568, 332, 568), fill="#c9d7cf", width=1)
    draw.text((28, 585), "FIXED DEMO / NOT AI GENERATED", fill="#647d71", font=font(14))
    encoder.stdin.write(image.tobytes())

encoder.stdin.close()
errors = encoder.stderr.read().decode(errors="replace")
if encoder.wait() != 0:
    raise SystemExit(errors)
print(f"Created {OUTPUT} ({OUTPUT.stat().st_size} bytes)")
