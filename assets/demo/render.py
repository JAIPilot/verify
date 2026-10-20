#!/usr/bin/env python3
"""Render the illustrated Spring Petclinic Verify walkthrough.

This is a product storyboard, not a recording or measurement of a live run.
The CLI labels, review order, and approval steps follow Verify's current flow.

Requires Pillow and ffmpeg. Run from anywhere:
    python assets/demo/render.py
"""

from __future__ import annotations

import subprocess
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[2]
ASSETS = ROOT / "assets"
SIZE = (1200, 675)
FPS = 7

BG = "#0a1019"
PANEL = "#111a27"
TERMINAL = "#0b111b"
EDGE = "#27374a"
WHITE = "#f2f7fd"
MUTED = "#9badc0"
CYAN = "#53d6d0"
GREEN = "#81dfac"
RED = "#ff8c98"
AMBER = "#ffd08a"
PURPLE = "#baacff"

def font_path(*candidates: str) -> str:
    for candidate in candidates:
        if Path(candidate).exists():
            return candidate
    raise RuntimeError("Install Helvetica Neue/Menlo or DejaVu Sans/Mono to render the demo")


SANS_PATH = font_path(
    "/System/Library/Fonts/HelveticaNeue.ttc",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
)
MONO_PATH = font_path(
    "/System/Library/Fonts/Menlo.ttc",
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
)
FONT = {
    "brand": ImageFont.truetype(SANS_PATH, 29),
    "title": ImageFont.truetype(SANS_PATH, 39),
    "body": ImageFont.truetype(SANS_PATH, 20),
    "small": ImageFont.truetype(SANS_PATH, 16),
    "tiny": ImageFont.truetype(SANS_PATH, 13),
    "mono": ImageFont.truetype(MONO_PATH, 18),
    "mono_small": ImageFont.truetype(MONO_PATH, 16),
    "mono_tiny": ImageFont.truetype(MONO_PATH, 14),
}


SCENES = [
    {
        "eyebrow": "01 / 07   CODEX",
        "title": "Build the feature with Codex",
        "subtitle": "Add owner lookup by phone to Spring Petclinic.",
        "tab": "codex  /  spring-framework-petclinic",
        "lines": [
            ("$ codex", CYAN),
            ("PROMPT", PURPLE),
            ("Add owner lookup by phone. Keep partial", WHITE),
            ("last-name search working as it does today.", WHITE),
            ("", MUTED),
            ("CHANGED FILES", PURPLE),
            ("M  OwnerController.java", AMBER),
            ("A  OwnerSearchService.java", GREEN),
            ("A  OwnerSearchTests.java", GREEN),
        ],
        "aside_title": "One feature. Two kinds of change.",
        "aside": ["New phone lookup", "Existing search path edited", "Old behavior needs protection"],
        "aside_emphasis": 2,
        "duration": 3.8,
    },
    {
        "eyebrow": "02 / 07   THE RISK",
        "title": "The new path has a hidden regression",
        "subtitle": "The feature works, but partial surname search no longer does.",
        "tab": "candidate behavior  /  owner search",
        "lines": [
            ("EXISTING BEHAVIOR", PURPLE),
            ('search "Dav"     →  Davis', GREEN),
            ("", MUTED),
            ("CODEX CHANGE", PURPLE),
            ('search "5551234567"  →  owner', GREEN),
            ('search "Dav"         →  no matches', RED),
            ("", MUTED),
            ("The diff looks small. The contract changed.", MUTED),
        ],
        "aside_title": "What should Verify do?",
        "aside": ["Establish the old behavior", "Replay it on the candidate", "Ask before changing intent"],
        "aside_emphasis": 1,
        "duration": 3.3,
    },
    {
        "eyebrow": "03 / 07   VERIFY · STAGE 1",
        "title": "Lock the behavior that came before",
        "subtitle": "Characterization tests are created on the baseline, then replayed.",
        "tab": "verify  /  baseline and replay",
        "lines": [
            ("$ verify --base main", CYAN),
            ("VERIFY  1 / 4   Characterize the baseline", PURPLE),
            ('✓  "Dav" finds Davis on main', GREEN),
            ("✓  Baseline tests pass", GREEN),
            ("", MUTED),
            ("Replay frozen tests on candidate", WHITE),
            ('✕  "Dav" expected Davis; got no matches', RED),
            ("", MUTED),
            ("Regression evidence is ready for review.", AMBER),
        ],
        "aside_title": "The key move",
        "aside": ["Test the old revision first", "Freeze those expectations", "Compare against the change"],
        "aside_emphasis": 1,
        "duration": 4.0,
    },
    {
        "eyebrow": "04 / 07   VERIFY · STAGE 2",
        "title": "Verify asks about your intent",
        "subtitle": "A failing replay can be a bug or a deliberate product decision.",
        "tab": "verify  /  decision",
        "lines": [
            ("VERIFY  2 / 4   Review intent and edits", PURPLE),
            ("╭─ DECISION", CYAN),
            ("│ Should partial surname matching remain?", WHITE),
            ("│ 1. Keep it; phone lookup is additive", MUTED),
            ("│ 2. Use exact surname matching", MUTED),
            ("╰────────────────────────────────────", CYAN),
            ("› Keep partial matching; add phone lookup.", GREEN),
            ("  ✓ Comment sent", GREEN),
        ],
        "aside_title": "You stay in the loop",
        "aside": ["Answer with a choice", "Or explain intent in your words", "Review the proposed edit"],
        "aside_emphasis": 1,
        "duration": 4.2,
    },
    {
        "eyebrow": "05 / 07   VERIFY · STAGE 2",
        "title": "See the repair before it is accepted",
        "subtitle": "Verify previews the proposed edit and waits for approval.",
        "tab": "verify  /  proposed edit",
        "lines": [
            ("Proposed edit · OwnerSearchService.java", CYAN),
            ("@@ owner matches query @@", PURPLE),
            ("- return lastName.equals(query);", RED),
            ("+ return lastName.startsWith(query);", GREEN),
            ("", MUTED),
            ("Accept this proposed edit?", WHITE),
            ("1. Yes    2. No", MUTED),
            ("› y", GREEN),
            ("  ✓ Yes", GREEN),
        ],
        "aside_title": "Human approval gate",
        "aside": ["Inspect the exact diff", "Accept or reject the proposal", "Approve the full candidate"],
        "aside_emphasis": 0,
        "duration": 3.9,
    },
    {
        "eyebrow": "06 / 07   VERIFY · STAGE 3",
        "title": "Lock the behavior you accepted",
        "subtitle": "The tester adds final tests and reruns local evidence checks.",
        "tab": "verify  /  final evidence",
        "lines": [
            ("VERIFY  3 / 4   Lock accepted behavior", PURPLE),
            ("✓  Partial surname search remains tested", GREEN),
            ("✓  New phone lookup gets its own tests", GREEN),
            ("", MUTED),
            ("Run project suite", WHITE),
            ("Measure affected-file coverage", WHITE),
            ("Run mutation checks", WHITE),
            ("", MUTED),
            ("Production code stays frozen in this stage.", CYAN),
        ],
        "aside_title": "Fresh evidence",
        "aside": ["Tests for old behavior", "Tests for new behavior", "Coverage and mutation gates"],
        "aside_emphasis": 2,
        "duration": 3.7,
    },
    {
        "eyebrow": "07 / 07   VERIFY · STAGE 4",
        "title": "Ship with a reviewable decision",
        "subtitle": "The report explains what was locked, changed, and checked.",
        "tab": "verify  /  report and promotion",
        "lines": [
            ("VERIFY  4 / 4   Prepare the report", PURPLE),
            ("✓  Baseline replay reviewed", GREEN),
            ("✓  Intent resolved and edit approved", GREEN),
            ("✓  Final evidence checked", GREEN),
            ("", MUTED),
            ("VERDICT   GOOD TO GO", GREEN),
            ("Final verified diff shown", WHITE),
            ("Apply verified changes to checkout?  y", CYAN),
        ],
        "aside_title": "From diff to decision",
        "aside": ["Evidence in one report", "Final diff before apply", "You choose when to promote"],
        "aside_emphasis": 1,
        "duration": 4.6,
    },
]


def text(draw: ImageDraw.ImageDraw, xy: tuple[int, int], value: str, *, font: str,
         fill: str, anchor: str | None = None) -> None:
    draw.text(xy, value, font=FONT[font], fill=fill, anchor=anchor)


def rounded(draw: ImageDraw.ImageDraw, box: tuple[int, int, int, int], *,
            fill: str, outline: str | None = None, radius: int = 18,
            width: int = 1) -> None:
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


def base_image() -> Image.Image:
    image = Image.new("RGB", SIZE, BG)
    draw = ImageDraw.Draw(image)
    # Static, low-contrast geometry gives the GIF a stable, compressible backdrop.
    draw.ellipse((740, -340, 1450, 370), fill="#0d1e2b")
    draw.ellipse((-390, 390, 290, 1040), fill="#111a2d")
    draw.line((60, 93, 1140, 93), fill=EDGE, width=1)
    rounded(draw, (60, 28, 103, 70), fill=CYAN, radius=12)
    text(draw, (81, 47), "V", font="brand", fill=BG, anchor="mm")
    text(draw, (118, 32), "verify", font="brand", fill=WHITE)
    text(draw, (244, 40), "by JAIPilot", font="small", fill=MUTED)
    text(draw, (1140, 38), "SPRING PETCLINIC  /  ILLUSTRATED WALKTHROUGH",
         font="tiny", fill=MUTED, anchor="ra")
    return image


def render_scene(index: int, elapsed: float) -> Image.Image:
    scene = SCENES[index]
    image = base_image()
    draw = ImageDraw.Draw(image)
    accent = CYAN if index < 2 else (GREEN if index == 6 else PURPLE)
    text(draw, (60, 118), scene["eyebrow"], font="small", fill=accent)
    text(draw, (60, 143), scene["title"], font="title", fill=WHITE)
    text(draw, (60, 193), scene["subtitle"], font="body", fill=MUTED)

    rounded(draw, (60, 238, 812, 585), fill=TERMINAL, outline=EDGE, radius=18)
    rounded(draw, (60, 238, 812, 282), fill="#182332", radius=18)
    draw.rectangle((60, 265, 812, 282), fill="#182332")
    for x, color in ((82, RED), (105, AMBER), (128, GREEN)):
        draw.ellipse((x, 255, x + 11, 266), fill=color)
    text(draw, (158, 251), scene["tab"], font="mono_tiny", fill=MUTED)

    # Stagger line reveals and use a cursor pulse on the current row.
    reveal = min(len(scene["lines"]), max(1, int((elapsed + 0.4) * 3.6)))
    y = 302
    for line_index, (content, color) in enumerate(scene["lines"][:reveal]):
        if content:
            text(draw, (84, y), content, font="mono_small", fill=color)
            if line_index == reveal - 1 and elapsed < scene["duration"] - 0.55 and int(elapsed * 3) % 2 == 0:
                width = draw.textlength(content, font=FONT["mono_small"])
                draw.rectangle((int(88 + width), y + 4, int(96 + width), y + 19), fill=CYAN)
        y += 30

    rounded(draw, (832, 238, 1140, 585), fill=PANEL, outline=EDGE, radius=18)
    rounded(draw, (854, 259, 882, 287), fill="#234a50", radius=8)
    text(draw, (868, 272), str(index + 1), font="small", fill=CYAN, anchor="mm")
    text(draw, (854, 309), scene["aside_title"], font="body", fill=WHITE)
    for item_index, item in enumerate(scene["aside"]):
        y = 370 + item_index * 53
        active = item_index == scene["aside_emphasis"]
        draw.ellipse((855, y + 6, 865, y + 16), fill=CYAN if active else "#536779")
        text(draw, (878, y), item, font="small", fill=WHITE if active else MUTED)
    text(draw, (855, 548), "You review each consequential step.",
         font="tiny", fill="#7890a6")

    # Timeline makes the loop understandable even when the GIF is paused.
    labels = ("CODEX", "RISK", "BASELINE", "INTENT", "EDIT", "LOCK", "REPORT")
    for part, label in enumerate(labels):
        x0 = 60 + part * 157
        fill = CYAN if part < index else (GREEN if part == index else "#2a3a4d")
        draw.rounded_rectangle((x0, 613, x0 + 143, 619), radius=3, fill=fill)
        text(draw, (x0, 630), label, font="tiny", fill=WHITE if part == index else MUTED)
    return image


def make_gif(name: str, order: list[int], durations: list[float]) -> None:
    with tempfile.TemporaryDirectory(prefix="verify-demo-") as temp:
        frames = Path(temp)
        frame_index = 0
        for scene_index, duration in zip(order, durations):
            count = round(duration * FPS)
            for step in range(count):
                elapsed = step / FPS
                render_scene(scene_index, elapsed).save(frames / f"frame-{frame_index:04d}.png")
                frame_index += 1
        output = ASSETS / name
        # One palette over the full sequence keeps colors consistent on GitHub.
        filter_graph = (
            "fps=7,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];"
            "[b][p]paletteuse=dither=bayer:bayer_scale=5"
        )
        subprocess.run(
            ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-framerate", str(FPS),
             "-i", str(frames / "frame-%04d.png"), "-filter_complex", filter_graph,
             "-loop", "0", str(output)],
            check=True,
        )
        print(f"{output.name}: {frame_index} frames, {output.stat().st_size / 1_000_000:.1f} MB")


if __name__ == "__main__":
    ASSETS.mkdir(exist_ok=True)
    make_gif("verify-petclinic-story.gif", list(range(7)), [s["duration"] for s in SCENES])
    make_gif("verify-review-loop.gif", [2, 3, 4, 5], [3.5, 4.2, 3.9, 3.5])
