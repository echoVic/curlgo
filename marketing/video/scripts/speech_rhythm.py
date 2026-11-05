"""Same silence threshold for old/new continuous narration; rhythm evidence only."""
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def measure(path):
    length = float(subprocess.check_output([
        'ffprobe', '-v', 'error', '-show_entries', 'format=duration',
        '-of', 'default=nw=1:nk=1', str(path)
    ], text=True))
    result = subprocess.run([
        'ffmpeg', '-hide_banner', '-i', str(path), '-af',
        'silencedetect=noise=-38dB:d=0.25', '-f', 'null', '-'
    ], check=True, capture_output=True, text=True)
    starts = [float(v) for v in re.findall(r'silence_start: ([\d.]+)', result.stderr)]
    ends = [float(v) for v in re.findall(r'silence_end: ([\d.]+)', result.stderr)]
    if len(ends) < len(starts):
        ends.append(length)
    intervals = [{'start': s, 'end': e, 'duration': e-s} for s, e in zip(starts, ends)]
    interior = [i for i in intervals if i['start'] > .1 and i['end'] < length-.1]
    return {
        'file': str(path.relative_to(ROOT)), 'durationSeconds': length,
        'silenceCount': len(intervals), 'silenceSeconds': sum(i['duration'] for i in intervals),
        'silencePercentage': 100*sum(i['duration'] for i in intervals)/length,
        'interiorPauseCount': len(interior),
        'interiorPauseSeconds': sum(i['duration'] for i in interior),
        'interiorPausePercentage': 100*sum(i['duration'] for i in interior)/length,
        'shortInteriorPauses025to07': sum(.25 <= i['duration'] <= .7 for i in interior),
        'intervals': intervals,
    }


report = {
    'thresholdDb': -38, 'minimumSilenceSeconds': .25,
    'old': measure(ROOT/'out/previous/roundtrip-20261104/public/narration/continuous.m4a'),
    'new': measure(ROOT/'public/narration/continuous.m4a'),
    'limitation': 'Silence detection measures rhythm, not perceived fluency. No human listening review is claimed. No detected silence was cut or time-compressed.',
}
(ROOT/'out/speech-rhythm-comparison.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
for label in ['old','new']:
    print(label, json.dumps({k:v for k,v in report[label].items() if k!='intervals'}, ensure_ascii=False))
