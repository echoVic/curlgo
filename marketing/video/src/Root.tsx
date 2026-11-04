import { Composition } from "remotion";
import { RoundtripFilm } from "./RoundtripFilm";
import narration from "../public/narration/manifest.json";

export const RemotionRoot: React.FC = () => (
  <Composition
    id="CurlGoRoundtrip"
    component={RoundtripFilm}
    durationInFrames={narration.durationInFrames}
    fps={30}
    width={1920}
    height={1080}
  />
);
