import { useCurrentFrame, useVideoConfig } from "remotion";
import captions from "../public/captions-zh.json";

// Same measured Caption-shaped JSON drives burned-in text and website VTT.
export const FilmCaptions = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const ms = (frame / fps) * 1000;
  const active = captions.find(
    (caption) => caption.startMs <= ms && ms < caption.endMs,
  );
  return (
    <div
      style={{
        position: "absolute",
        left: 170,
        right: 170,
        bottom: 57,
        height: 74,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {active ? (
        <div
          style={{
            fontSize: 34,
            lineHeight: 1.5,
            fontWeight: 450,
            color: "#f0fff9",
            background: "#06242be6",
            borderRadius: 12,
            padding: "10px 27px",
            textShadow: "0 2px 4px #00191f",
            textAlign: "center",
          }}
        >
          {active.text}
        </div>
      ) : null}
    </div>
  );
};
