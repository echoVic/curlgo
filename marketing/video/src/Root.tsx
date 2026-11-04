import { Composition, Folder } from "remotion";
import { CurlGoLaunch } from "./CurlGoLaunch";
import { Intro } from "./scenes/Intro";
import { Native } from "./scenes/Native";
import { Migration } from "./scenes/Migration";
import { Assertions } from "./scenes/Assertions";
import { Agents } from "./scenes/Agents";
import { Outro } from "./scenes/Outro";

export const RemotionRoot: React.FC = () => {
  return (
    <>
      <Composition
        id="CurlGoLaunch"
        component={CurlGoLaunch}
        durationInFrames={2100}
        fps={30}
        width={1920}
        height={1080}
      />
      <Folder name="Scenes">
        <Composition
          id="Intro"
          component={Intro}
          durationInFrames={210}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Native"
          component={Native}
          durationInFrames={450}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Migration"
          component={Migration}
          durationInFrames={330}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Assertions"
          component={Assertions}
          durationInFrames={420}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Agents"
          component={Agents}
          durationInFrames={450}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Outro"
          component={Outro}
          durationInFrames={300}
          fps={30}
          width={1920}
          height={1080}
        />
      </Folder>
    </>
  );
};
