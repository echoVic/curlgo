"""One continuous Edge TTS take; original speed, measured word boundaries.

Only public copy is sent to Edge. No account credentials are needed. A cached
raw take is reusable only when the script and voice settings match its hash.
The composition follows the natural audio duration, never the reverse.
"""

import asyncio
import hashlib
import json
from pathlib import Path
import re
import subprocess

import edge_tts

ROOT = Path(__file__).resolve().parents[1]
CONFIG = json.loads((ROOT / "narration.json").read_text())
OUTPUT = ROOT / "public" / "narration"
RAW = ROOT / "out" / "narration-raw"
OUTPUT.mkdir(parents=True, exist_ok=True)
RAW.mkdir(parents=True, exist_ok=True)


def normalize(text):
    return re.sub(r"[\W_]+", "", text, flags=re.UNICODE).lower()


def duration(path):
    return float(subprocess.check_output([
        "ffprobe", "-v", "error", "-show_entries", "format=duration",
        "-of", "default=nw=1:nk=1", str(path)
    ], text=True).strip())


def stamp(ms):
    hours, rem = divmod(round(ms), 3600000)
    minutes, rem = divmod(rem, 60000)
    seconds, millis = divmod(rem, 1000)
    return f"{hours:02}:{minutes:02}:{seconds:02}.{millis:03}"


async def main():
    text = "".join(p["text"] for p in CONFIG["phrases"])
    # Stay below Edge's 4096-byte split threshold: one service stream/take.
    if len(text.encode("utf-8")) > 3500:
        raise ValueError("Shorten the copy to keep a single service take")
    digest = hashlib.sha256(json.dumps(CONFIG, sort_keys=True).encode()).hexdigest()
    raw = RAW / f"continuous-{digest[:16]}.mp3"
    boundaries_path = RAW / f"continuous-{digest[:16]}-boundaries.json"
    if not raw.exists() or not boundaries_path.exists():
        boundaries = []
        communicate = edge_tts.Communicate(
            text, CONFIG["voice"], rate=CONFIG["rate"], pitch=CONFIG["pitch"],
            boundary="WordBoundary",
        )
        with raw.open("wb") as out:
            async for chunk in communicate.stream():
                if chunk["type"] == "audio":
                    out.write(chunk["data"])
                elif chunk["type"] == "WordBoundary":
                    boundaries.append({k: chunk[k] for k in ["offset", "duration", "text"]})
        boundaries_path.write_text(json.dumps(boundaries, ensure_ascii=False, indent=2) + "\n")
    boundaries = json.loads(boundaries_path.read_text())
    if normalize("".join(w["text"] for w in boundaries)) != normalize(text):
        raise ValueError("Word boundaries do not cover the complete spoken script")
    lead_ms = CONFIG["leadInSeconds"] * 1000
    cursor = 0
    words = []
    for word in boundaries:
        n = len(normalize(word["text"]))
        words.append({
            "text": word["text"], "startMs": lead_ms + word["offset"] / 10000,
            "endMs": lead_ms + (word["offset"] + word["duration"]) / 10000,
            "timestampMs": lead_ms + word["offset"] / 10000,
            "confidence": None, "charStart": cursor, "charEnd": cursor + n,
        })
        cursor += n
    captions, beats = [], []
    cursor = 0
    for phrase in CONFIG["phrases"]:
        end = cursor + len(normalize(phrase["text"]))
        included = [w for w in words if w["charEnd"] > cursor and w["charStart"] < end]
        if not included:
            raise ValueError(f"No boundaries for {phrase['text']}")
        start_ms, end_ms = included[0]["startMs"], included[-1]["endMs"]
        captions.append({
            "text": phrase.get("caption", phrase["text"].rstrip("，。；：")),
            "startMs": round(start_ms), "endMs": round(end_ms),
            "timestampMs": round(start_ms), "confidence": None,
        })
        if "beat" in phrase:
            beats.append({"id": phrase["beat"], "seconds": round(max(0, start_ms / 1000 - .45), 3)})
        cursor = end
    # A small reading hold follows the measured last word, without overlap.
    for i, cap in enumerate(captions):
        next_start = captions[i+1]["startMs"] if i+1 < len(captions) else float("inf")
        cap["endMs"] = min(cap["endMs"] + 160, next_start)
    target = OUTPUT / "continuous.m4a"
    subprocess.run([
        "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(raw),
        "-af", "loudnorm=I=-16:TP=-2:LRA=7", "-ar", "48000",
        "-c:a", "aac", "-b:a", "192k", str(target)
    ], check=True)
    natural = duration(raw)
    processed = duration(target)
    if abs(natural - processed) > .1:
        raise ValueError("Normalization changed the duration unexpectedly")
    total = CONFIG["leadInSeconds"] + processed + CONFIG["tailSeconds"]
    frames = round(total * 30 + .499999)
    manifest = {
        "voice": CONFIG["voice"], "rate": CONFIG["rate"], "pitch": CONFIG["pitch"],
        "takeCount": 1, "playbackRate": 1, "timeStretch": False,
        "scriptSha256": digest, "audio": "narration/continuous.m4a",
        "from": CONFIG["leadInSeconds"], "duration": processed,
        "rawDuration": natural, "end": CONFIG["leadInSeconds"] + processed,
        "durationInFrames": frames, "fps": 30, "durationSeconds": frames / 30,
        "boundarySource": "Edge TTS WordBoundary from this exact audio stream",
        "wordCount": len(words), "captionCount": len(captions), "beats": beats,
    }
    (OUTPUT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    (OUTPUT / "words-zh.json").write_text(json.dumps(words, ensure_ascii=False, indent=2) + "\n")
    (ROOT / "public/captions-zh.json").write_text(json.dumps(captions, ensure_ascii=False, indent=2) + "\n")
    vtt = ["WEBVTT", ""]
    for i, cap in enumerate(captions, 1):
        vtt += [str(i), f"{stamp(cap['startMs'])} --> {stamp(cap['endMs'])}", cap["text"], ""]
    (ROOT / "public/captions-zh.vtt").write_text("\n".join(vtt))
    print(f"One natural take: {processed:.3f}s; film {frames/30:.3f}s; {len(words)} measured words, {len(captions)} captions", flush=True)
    print(json.dumps(beats, ensure_ascii=False, indent=2))


asyncio.run(main())
