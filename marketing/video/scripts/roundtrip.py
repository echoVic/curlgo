"""Original CurlGo 'Roundtrip' score: paired plucks, soft bass, spacious pulse.

The two-note call/response motif follows the selected loop identity. All audio
is synthesized from sine oscillators; no music samples are used.
Requires NumPy. Usage: python3 scripts/roundtrip.py out/roundtrip.wav
"""

import json
from pathlib import Path
import sys
import wave

import numpy as np

RATE = 44100
MANIFEST = json.loads((Path(__file__).resolve().parents[1] / "public/narration/manifest.json").read_text())
DURATION = MANIFEST["durationSeconds"]
BPM = 72
BEAT = 60 / BPM
ROOT = Path(__file__).resolve().parents[1]
music = np.zeros((round(RATE * DURATION), 2), dtype=np.float64)


def hz(note):
    return 440 * 2 ** ((note - 69) / 12)


def add(time, signal, level=1, pan=0):
    start = round(time * RATE)
    end = min(len(music), start + len(signal))
    if start >= end:
        return
    phase = (pan + 1) * np.pi / 4
    music[start:end, 0] += signal[:end-start] * level * np.cos(phase)
    music[start:end, 1] += signal[:end-start] * level * np.sin(phase)


def pluck(note, length=1.3):
    t = np.arange(round(RATE * length)) / RATE
    f = hz(note)
    envelope = np.minimum(1, t / .035) * np.exp(-t * 2.9)
    return envelope * (np.sin(2*np.pi*f*t) + .06*np.sin(2*np.pi*2*f*t))


chords = [(50, 57, 62, 66, 69), (47, 54, 59, 62, 66), (43, 50, 55, 59, 62), (45, 52, 57, 61, 64)]
for bar in range(int(np.ceil(DURATION / (BEAT * 4)))):
    start = bar * BEAT * 4
    chord = chords[(bar // 2) % 4]
    t = np.arange(round(RATE * BEAT * 4.5)) / RATE
    env = np.minimum(1, t/.3) * np.clip((BEAT*4.5-t)/.7, 0, 1)
    pad = sum(np.sin(2*np.pi*hz(n+12)*t) for n in chord[1:]) / 4
    add(start, pad * env, .024, -.1)
    if bar > 1:
        for beat in [0]:
            tb = np.arange(round(RATE*.7))/RATE
            bass = np.sin(2*np.pi*hz(chord[0]-12)*tb) * np.minimum(1,tb/.012) * np.exp(-tb*5)
            add(start+beat*BEAT, bass, .040)
    # A short question on the left; its answer arrives on the right.
    note = chord[3]
    if bar % 2 == 0:
        add(start + BEAT*.5, pluck(note, 2.0), .065, -.2)
        add(start + BEAT*2, pluck(chord[2], 2.0), .050, .2)

time = np.arange(len(music)) / RATE
# Duck from the real continuous waveform, not obsolete scene windows.
# 20 ms RMS windows, 120 ms look-ahead attack and a 650 ms release.
import subprocess
speech = subprocess.check_output([
    "ffmpeg", "-v", "error", "-i", str(ROOT / "public" / MANIFEST["audio"]),
    "-f", "f32le", "-ac", "1", "-ar", str(RATE), "-"
])
speech = np.frombuffer(speech, dtype="<f4")
window = round(RATE * .02)
rms = np.array([np.sqrt(np.mean(speech[i:i+window] ** 2)) for i in range(0, len(speech), window)])
# Normalized speech below -40 dB is silence. Keep bed low over short pauses.
activity = np.clip((rms - .01) / .035, 0, 1)
attack, release = 6, 33
smoothed = np.zeros_like(activity)
for i in range(len(activity)):
    future = np.max(activity[i:min(len(activity), i+attack+1)])
    previous = smoothed[i-1] if i else 0
    smoothed[i] = max(future, previous - 1/release)
speech_times = MANIFEST["from"] + np.arange(len(activity)) * .02
active = np.interp(time, speech_times, smoothed, left=0, right=0)
duck = 1 - .80 * active
fade = np.minimum(np.clip(time/1.2,0,1), np.clip((DURATION-time)/2.2,0,1))
music *= (.8 * duck * fade)[:,None]
peak = np.abs(music).max()
if peak > .8:
    music *= .8/peak
(ROOT / "public/score-manifest.json").write_text(json.dumps({
    "title": "Roundtrip", "videoRevision": MANIFEST["videoRevision"],
    "arrangement": "72 BPM; half-density paired motif an octave lower; soft attack; no percussion", "originalSynthesis": True, "bpm": BPM,
    "durationSeconds": DURATION, "duckSource": MANIFEST["audio"],
    "duckMethod": "20ms waveform RMS, 120ms look-ahead, 650ms release",
    "maximumReductionDb": round(20*np.log10(.2), 2),
    "minimumGain": float(duck.min()), "maximumGain": float(duck.max()),
    "speechActiveSeconds": round(float(np.sum(active>.5)/RATE), 3),
    "peak": round(float(np.abs(music).max()), 5),
}, indent=2) + "\n")
with wave.open(sys.argv[1], "wb") as out:
    out.setnchannels(2)
    out.setsampwidth(2)
    out.setframerate(RATE)
    out.writeframes((np.clip(music,-1,1)*32767).astype('<i2').tobytes())
print(f"Roundtrip: {DURATION}s stereo, {BPM} BPM, peak {np.abs(music).max():.3f}")
