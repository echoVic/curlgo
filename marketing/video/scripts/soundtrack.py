"""Create the original 70-second CurlGo ambient score using only the stdlib."""
import array
import math
import sys
import wave

rate = 32000
duration = 70
chords = [(220, 277.183, 329.628), (174.614, 220, 261.626),
          (130.813, 164.814, 195.998), (195.998, 246.942, 293.665)]
notes = [659.255, 554.365, 440, 329.628, 523.251, 440, 349.228, 261.626]
samples = array.array("h")
for index in range(rate * duration):
    t = index / rate
    chord = chords[int(t / 4) % len(chords)]
    fade = min(1, t / 2, (duration - t) / 3)
    pad = sum(math.sin(2 * math.pi * freq * t) + 0.2 * math.sin(2 * math.pi * freq * 1.003 * t) for freq in chord)
    beat = t % (60 / 112)
    pulse = math.exp(-beat * 18) * math.sin(2 * math.pi * 55 * t)
    pluck_age = t % (60 / 112 * 2)
    pluck = math.exp(-pluck_age * 4) * math.sin(2 * math.pi * notes[int(t / (60 / 112 * 2)) % len(notes)] * t)
    value = fade * (0.035 * pad + 0.095 * pulse + 0.05 * pluck)
    samples.append(int(max(-1, min(1, value)) * 32767))
with wave.open(sys.argv[1], "wb") as output:
    output.setnchannels(1)
    output.setsampwidth(2)
    output.setframerate(rate)
    output.writeframes(samples.tobytes())
