# CurlGo launch video

A 70-second, 1920 × 1080, 30 fps Remotion film for CurlGo v0.1.0.

## Preview and render

```sh
npm ci
npm run dev
npm run lint
npm run render
```

Output: `out/curlgo-0.1.0-launch.mp4` (H.264 + AAC).

## Chapters

| Start | Content                                     |
| ----- | ------------------------------------------- |
| 00:00 | Your API tool, your choice                  |
| 00:07 | Native client: HTTP, SSE and WebSocket      |
| 00:21 | curl, Postman v2.1 and Bruno migration      |
| 00:32 | Assertions and response variable extraction |
| 00:45 | Explicit, optional agent skill installation |
| 01:00 | MIT open-source release                     |

The UI images are CurlGo's generated demo workspace and a local test-interface screenshot. The migration preview is a labelled illustration. This is a composed product film, not a continuous screen recording.

The supplied reference inspired the light background, gradient headings, numbered chapters and segmented progress strip. None of its footage, branding or audio is redistributed.

The original procedural ambient score is MIT licensed. To regenerate it (Python and FFmpeg required):

```sh
python3 scripts/soundtrack.py /tmp/curlgo-score.wav
ffmpeg -i /tmp/curlgo-score.wav -c:a aac -b:a 160k public/soundtrack.m4a
```

Remotion's dependencies retain their respective licenses; the repository MIT license covers original CurlGo video sources and assets. Rendering requires an installed Chromium or Remotion's downloaded Chrome Headless Shell. Chinese typography uses system fonts without redistributing font binaries.
