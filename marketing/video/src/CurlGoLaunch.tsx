import { Audio } from "@remotion/media";
import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { AbsoluteFill, interpolate, staticFile } from "remotion";
import { Progress } from "./shared";
import { Intro } from "./scenes/Intro";
import { Native } from "./scenes/Native";
import { Migration } from "./scenes/Migration";
import { Assertions } from "./scenes/Assertions";
import { Agents } from "./scenes/Agents";
import { Outro } from "./scenes/Outro";

export const CurlGoLaunch = () => (
  <AbsoluteFill>
    <TransitionSeries>
      <TransitionSeries.Sequence durationInFrames={210}>
        <Intro />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 12 })}
      />
      <TransitionSeries.Sequence durationInFrames={450}>
        <Native />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 12 })}
      />
      <TransitionSeries.Sequence durationInFrames={330}>
        <Migration />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 12 })}
      />
      <TransitionSeries.Sequence durationInFrames={420}>
        <Assertions />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 12 })}
      />
      <TransitionSeries.Sequence durationInFrames={450}>
        <Agents />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 12 })}
      />
      <TransitionSeries.Sequence durationInFrames={300}>
        <Outro />
      </TransitionSeries.Sequence>
    </TransitionSeries>
    <Audio
      src={staticFile("soundtrack.m4a")}
      volume={(frame) =>
        interpolate(frame, [0, 45, 2025, 2100], [0, 0.65, 0.65, 0], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        })
      }
    />
    <Progress />
  </AbsoluteFill>
);
