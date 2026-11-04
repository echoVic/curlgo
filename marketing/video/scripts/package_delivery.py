"""Package the finished Remotion render for a website and verify its media.

Requires FFmpeg/ffprobe. Writes exclusively inside this marketing/video project.
The website consumes public/curlgo-roundtrip-zh.mp4, curlgo-poster.jpg,
captions-zh.vtt and video-metadata.json. No upload/deployment is performed.
"""

import hashlib
import json
from pathlib import Path
import shutil
import struct
import subprocess

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "out"
PUBLIC = ROOT / "public"


def run(args):
    return subprocess.run(args, check=True, text=True, capture_output=True)


def atoms(path):
    result = []
    with path.open("rb") as file:
        while True:
            offset = file.tell()
            header = file.read(8)
            if len(header) < 8:
                break
            size, kind = struct.unpack(">I4s", header)
            if size == 1:
                size = struct.unpack(">Q", file.read(8))[0]
            result.append({"type": kind.decode("ascii"), "offset": offset, "size": size})
            if not size:
                break
            file.seek(offset + size)
    return result


def digest(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def main():
    source = OUT / "curlgo-roundtrip-zh.mp4"
    staged = OUT / "curlgo-roundtrip-faststart.mp4"
    source_probe = json.loads(run(["ffprobe", "-v", "error", "-select_streams", "v:0",
                                   "-show_entries", "stream=pix_fmt,color_range", "-of", "json", str(source)]).stdout)
    source_video = source_probe["streams"][0]
    if source_video["pix_fmt"] == "yuv420p" and source_video.get("color_range") == "tv":
        options = ["-c", "copy"]
    else:
        options = ["-vf", "scale=in_range=full:out_range=tv:out_color_matrix=bt709,format=yuv420p",
                   "-c:v", "libx264", "-preset", "fast", "-crf", "18",
                   "-color_range", "tv", "-colorspace", "bt709", "-color_primaries", "bt709",
                   "-color_trc", "bt709", "-c:a", "copy"]
    run(["ffmpeg", "-v", "error", "-y", "-i", str(source),
         *options, "-movflags", "+faststart", str(staged)])
    staged.replace(source)
    target = PUBLIC / source.name
    shutil.copy2(source, target)
    probe = json.loads(run(["ffprobe", "-v", "error", "-show_streams", "-show_format",
                           "-of", "json", str(target)]).stdout)
    video = next(s for s in probe["streams"] if s["codec_type"] == "video")
    audio = next(s for s in probe["streams"] if s["codec_type"] == "audio")
    narration = json.loads((PUBLIC / "narration/manifest.json").read_text())
    assert video["codec_name"] == "h264" and video["pix_fmt"] == "yuv420p"
    assert video["width"] == 1920 and video["height"] == 1080
    assert video["r_frame_rate"] == "30/1"
    assert int(video["nb_frames"]) == narration["durationInFrames"]
    assert abs(float(video["duration"]) - narration["durationSeconds"]) < .04
    assert audio["codec_name"] == "aac"
    assert narration["playbackRate"] == 1 and not narration["timeStretch"]
    top_atoms = atoms(target)
    moov = next(a["offset"] for a in top_atoms if a["type"] == "moov")
    mdat = next(a["offset"] for a in top_atoms if a["type"] == "mdat")
    assert moov < mdat, "moov must precede mdat for faststart"
    decode = run(["ffmpeg", "-v", "error", "-i", str(target), "-f", "null", "-"])
    assert not decode.stderr.strip(), decode.stderr
    caps = json.loads((PUBLIC / "captions-zh.json").read_text())
    for i, cap in enumerate(caps):
        assert cap["text"] and 0 <= cap["startMs"] < cap["endMs"] <= narration["end"] * 1000
        if i:
            assert caps[i-1]["endMs"] <= cap["startMs"]
    assert (PUBLIC / "captions-zh.vtt").read_text().count(" --> ") == len(caps)
    stills = [(5.0, "opening"), (15.5, "request"), (29.5, "response"),
              (34.5, "verify"), (38.8, "extract"), (48.0, "skill"),
              (54.5, "preflight"), (63.5, "closing"), (68.4, "end")]
    for seconds, label in stills:
        run(["ffmpeg", "-v", "error", "-y", "-ss", str(seconds), "-i", str(target),
             "-frames:v", "1", str(OUT / f"key-{label}.png")])
    run(["ffmpeg", "-v", "error", "-y", "-ss", "15.5", "-i", str(target),
         "-frames:v", "1", "-q:v", "2", str(PUBLIC / "curlgo-poster.jpg")])
    loudness = run(["ffmpeg", "-hide_banner", "-i", str(target), "-af",
                    "loudnorm=I=-16:TP=-1:LRA=11:print_format=json", "-f", "null", "-"])
    measured, _ = json.JSONDecoder().raw_decode(loudness.stderr[loudness.stderr.rfind("{"):])
    assert float(measured["input_tp"]) <= -1
    metadata = {
        "title": "CurlGo — 从一条请求开始", "version": "0.1.1",
        "composition": "CurlGoRoundtrip", "durationSeconds": float(video["duration"]),
        "width": video["width"], "height": video["height"], "fps": 30,
        "frames": int(video["nb_frames"]), "videoCodec": "h264",
        "audioCodec": "aac", "pixelFormat": "yuv420p", "faststart": True,
        "moovOffset": moov, "mdatOffset": mdat,
        "bytes": target.stat().st_size, "sha256": digest(target),
        "video": target.name, "poster": "curlgo-poster.jpg",
        "posterFrameSeconds": 15.5, "captions": "captions-zh.vtt",
        "language": "zh-CN", "captionCount": len(caps),
        "narration": {k: narration[k] for k in ["voice", "rate", "pitch", "takeCount", "playbackRate", "timeStretch", "duration", "boundarySource"]},
        "posterSha256": digest(PUBLIC / "curlgo-poster.jpg"),
        "captionsSha256": digest(PUBLIC / "captions-zh.vtt"),
        "audioMeasured": {"integratedLufs": float(measured["input_i"]), "truePeakDbtp": float(measured["input_tp"]), "lra": float(measured["input_lra"])},
        "validation": {"webColorRange": "limited BT.709 yuv420p", "audioCopyNoTimeChange": True, "fullDecode": "passed", "captionsOrderedNonoverlapping": True,
                       "naturalSpeechRate": True, "faststartAtoms": "passed",
                       "renderedKeyStills": [{"seconds": s, "file": f"out/key-{label}.png"} for s, label in stills]},
        "website": {"preload": "none", "controls": True, "playsInline": True,
                    "trackKind": "captions", "trackLabel": "简体中文"},
        "claims": "Native workspace is real; response data and CLI interaction are labelled illustrations. Closing version is 0.1.1; no updater availability claim.",
    }
    (PUBLIC / "video-metadata.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
    (OUT / "SHA256SUMS").write_text("\n".join(f"{digest(PUBLIC / name)}  public/{name}" for name in [target.name, "curlgo-poster.jpg", "captions-zh.vtt", "video-metadata.json"]) + "\n")
    print(json.dumps(metadata, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
