# CurlGo — flow-20261105

The active `CurlGoRoundtrip` composition is **51.4 seconds**, 1920 × 1080, 30 fps, 1542 frames. It retains the selected A double-loop identity and identifies **v0.1.1**, without claiming earlier versions can update themselves.

The entire film stays on solid petrol `#0c3038`. One native dark workbench remains mounted from first frame to last. The camera only advances from scale 1 to 1.018, without panning, returning moves, whole-window fades or page replacement. Only two title sentences appear: the opening fades away by 7.5 seconds and the closing enters near the end. Address entry, sending, response, assertions and variable extraction develop within the same workbench. The optional CLI illustration enters as an ancillary drawer.

`public/curlgo-dark.png` was produced by the existing built CurlGo 0.1.1 macOS app using disposable `--dark --render` at 1200 × 820; no application source was changed. Native theme/status controls are darkened. The cropped sidebar footer is cleared only at native coordinates x=0, y=628, width=227, height=32 using its measured background color #181826; the frame and response viewport geometry stay unchanged. The persistent label says **“原生深色界面截图 · 地址、响应与交互为示意，非真实录屏”**. The CLI drawer labels itself as an illustration and requires separate Skill installation and a CurlGo executable. Response time/data are fictional demonstration values. No footage or audio from the supplied reference is used.

Chinese narration is **one whole-script Edge TTS take**, `zh-CN-XiaoxiaoNeural`, `+0%`, neutral pitch, playback rate 1. Natural duration is 48.4 seconds; the film adds 0.6 seconds before it and 2.4 seconds after it. Four narrative paragraphs follow one health-check request through response, verification, extraction and optional command-line checking. Captions display standard technical names; speech uses “Curl Go”, “两百”, “接口测试技能” and “命令行” to avoid an acronym-heavy feature list. No fitting, stretching, silence trimming or sentence-level audio assembly occurs.

The 124 Edge WordBoundary measurements from that exact stream remain in `public/narration/words-zh.json`. The 17 captions begin at measured boundaries and hold exactly until the next cue starts; the last holds 400 ms after its final measured word. Burned-in captions share rounded frame boundaries at start and end, preventing fractional-frame blanks. VTT timings use the same data.

The original synthesized Roundtrip motif remains at 72 BPM with lower, half-density paired notes, softened attacks, fewer harmonics and no percussion. Music uses 20 ms RMS measurements of actual narration, with 120 ms look-ahead and 650 ms release, ducking by up to 13.98 dB. `public/score-manifest.json` records arrangement and gain evidence.

## Rhythm comparison

Both narration files use the same FFmpeg filter: `silencedetect=noise=-38dB:d=0.25`. Interior pauses exclude boundary silence. Percentage denominators are each narration file's full duration.

| Measurement | Previous 65.3 s narration | New 48.4 s narration |
| --- | ---: | ---: |
| All detected silence intervals | 40 | 9 |
| All detected silence duration | 19.544 s / 29.93% | 5.831 s / 12.05% |
| Interior pauses | 39 | 8 |
| Interior pause duration | 18.643 s / 28.55% | 5.237 s / 10.82% |
| Interior pauses lasting 0.25–0.7 s | 39 | 8 |

Complete intervals are in `out/speech-rhythm-comparison.json`. Measurements describe pause density, not perceived fluency. No human listening review is claimed.

## Generate, render and package

```sh
python3 scripts/narration.py
python3 scripts/speech_rhythm.py
python3 scripts/roundtrip.py out/roundtrip.wav
ffmpeg -y -i out/roundtrip.wav -c:a aac -b:a 192k public/roundtrip.m4a
npm run lint
npm run render -- --browser-executable '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' --concurrency=4
python3 scripts/package_delivery.py
```

Preview: `npm run dev -- --port=3218`, then open `http://localhost:3218/CurlGoRoundtrip`. The generator caches the original stream and measurements inside ignored `out/`; natural-rate audio only receives loudness normalization.

## Website handoff

- `public/curlgo-roundtrip-zh.mp4` — H.264, limited-range BT.709 yuv420p, AAC, faststart; identical to `out/curlgo-roundtrip-zh.mp4` after packaging.
- `public/curlgo-poster.jpg` — final encoded frame at 23.5 seconds.
- `public/captions-zh.vtt` — 17 contiguous measured Chinese captions.
- `public/video-metadata.json` — revision, duration, codecs, bytes, hashes, loudness, caption continuity, rhythm comparison, visual provenance and validation limits.
- `out/SHA256SUMS` — video, poster, VTT and metadata hashes.
- `out/key-*.png` — nine encoded stills at 1, 5.5, 12, 18, 23.5, 34, 40, 48 and 51 seconds.
- `out/flow-final-contactsheet.jpg` and `out/flow-20261105-report.md` — visual evidence and report.
- `out/previous/roundtrip-20261104/` — ignored archive of previous 69-second film, assets, narration and source.

Packaging converts full-range render pixels to limited-range BT.709 if needed, copies audio without timing changes, and moves `moov` before `mdat`. It checks full decode, exact duration/frame count, H.264/AAC, caption adjacency and zero interior blank caption frames. It verifies the petrol corner is identical across all 1542 encoded frames. Key stills are inspected separately for clipping and title/caption overlap.

The website owner should copy all four public delivery files together to a **new versioned asset URL**, then check production HTTP Range and browser play/seek. Suggested markup uses `controls playsinline preload="none"`, the poster and a Chinese captions track. Public filenames remain unchanged for handoff. Local faststart validation does not prove production Range support.

This task writes only `marketing/video/**` and does not commit, push, upload or deploy. Original CurlGo code, identity and procedural score use MIT; dependencies and narration retain their applicable terms. System Chinese fonts are used without redistributing font binaries.
