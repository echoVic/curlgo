import { useCurrentFrame, useVideoConfig } from "remotion";
import captions from "../public/captions-zh.json";

// All cue times round to the same frame boundary in/out. Text replaces text
// directly, with no fade and no short black intervals between speech clauses.
export const FilmCaptions = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const active = captions.find(
    (c) =>
      Math.round((c.startMs / 1000) * fps) <= frame &&
      frame < Math.round((c.endMs / 1000) * fps),
  );
  return (
    <div
      style={{
        position: "absolute",
        left: 180,
        right: 180,
        bottom: 39,
        height: 62,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        fontSize: 33,
        lineHeight: 1.4,
        color: "#dbe4e8",
        textAlign: "center",
      }}
    >
      {active?.text ?? ""}
    </div>
  );
};
