# CurlGo Roundtrip — revised Chinese film

The active `CurlGoRoundtrip` composition is 1920 × 1080, 30 fps, **69 seconds**. It retains the selected A double-loop identity. Its closing identifies **v0.1.1** without claiming older builds can update themselves.

The revision uses one continuous Edge TTS take at `+0%`, neutral pitch, playback rate 1. There is no speech fitting or time stretching. The natural 65.3-second narration sets the film length, with 0.7 seconds of lead-in and 3 seconds of closing hold. The conversational copy follows request → response → business verification → variable extraction → optional API Test Skill.

A single petrol stage and a persistent native workspace replace the alternating paper/petrol title cards. The camera pans and scales smoothly, detail layers crossfade, and the completed response stays visible through the optional Skill story. Text sits beside the product. The screenshot is the existing disposable CurlGo demo workspace; response data, assertion layout and CLI interaction are clearly labelled illustrations. This composed film uses no footage or audio from the supplied reference.

## Generate and render

```sh
python3 scripts/narration.py
python3 scripts/roundtrip.py out/roundtrip.wav
ffmpeg -y -i out/roundtrip.wav -c:a aac -b:a 192k public/roundtrip.m4a
npm run lint
npm run render -- --browser-executable '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' --concurrency=4
python3 scripts/package_delivery.py
```

Preview: `npm run dev -- --port=3218`, then open `http://localhost:3218/CurlGoRoundtrip`.

Edge TTS returns word boundaries from the exact continuous audio stream. `public/narration/words-zh.json` retains those measurements. Phrase captions in `public/captions-zh.json` use the Remotion Caption shape and generate both burned-in captions and `public/captions-zh.vtt`; each reading hold is capped before the next cue. Visual beats are derived from the measured phrase starts in `public/narration/manifest.json`.

The original **Roundtrip** synthesized score retains its 96 BPM paired-pluck motif. `scripts/roundtrip.py` measures the actual continuous speech waveform in 20 ms RMS windows, with 120 ms look-ahead and 650 ms release, reducing the score by up to 14 dB beneath speech. `public/score-manifest.json` records its timing and gain evidence. No music samples are used.

## Website handoff

- `public/curlgo-roundtrip-zh.mp4` — H.264, limited-range BT.709 yuv420p, AAC, faststart; identical to `out/curlgo-roundtrip-zh.mp4` after packaging.
- `public/curlgo-poster.jpg` — frame at 15.5 seconds.
- `public/captions-zh.vtt` — measured Chinese captions, 28 cues.
- `public/video-metadata.json` — duration, dimensions, codecs, byte count, SHA-256, loudness, checks and website settings.
- `out/SHA256SUMS` — video, poster, VTT and metadata hashes.
- `out/key-*.png` — nine stills extracted from the final encoded video.

The package script converts the full-range Remotion render to browser-compatible limited-range BT.709 yuv420p, copies audio without timing changes, enables faststart, and verifies all video/audio frames decode, exact composition duration/frame count, H.264/AAC, caption ordering and coverage, and that the MP4 `moov` atom precedes `mdat`. Website integration can use the files above with:

```html
<video controls playsinline preload="none" poster="/video/curlgo-poster.jpg">
  <source src="/video/curlgo-roundtrip-zh.mp4" type="video/mp4" />
  <track
    kind="captions"
    src="/video/captions-zh.vtt"
    srclang="zh-CN"
    label="简体中文"
  />
</video>
```

The film already contains Chinese captions; the VTT offers an additional accessible track. Asset URLs are examples for the website owner to map to deployed paths. This task prepares local assets only and does not commit, push, upload or deploy.

Original CurlGo code, identity and procedural score use MIT. Remotion dependencies and synthesized narration retain their applicable licences/terms. Chinese typography uses system fonts without redistributing font binaries.
