import { Audio } from "@remotion/media";
import {
  AbsoluteFill,
  Easing,
  Img,
  interpolate,
  Sequence,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import manifest from "../public/narration/manifest.json";
import { FilmCaptions } from "./FilmCaptions";

const petrol = "#0c3038";
const mint = "#42d9af";
const foreground = "#dbe4e8";
const muted = "#98a8af";
const panel = "#161724";
const border = "#303343";
const mono = '"SFMono-Regular", Menlo, monospace';
const smooth = {
  extrapolateLeft: "clamp" as const,
  extrapolateRight: "clamp" as const,
  easing: Easing.bezier(0.3, 0, 0.25, 1),
};
const at = (id: string) => {
  const beat = manifest.beats.find((b) => b.id === id);
  if (!beat) throw new Error(`Missing narration beat ${id}`);
  return beat.seconds;
};
const reveal = (t: number, id: string, delay = 0, length = 1) =>
  interpolate(t, [at(id) + delay, at(id) + delay + length], [0, 1], smooth);

// A single mounted native screenshot and in-workspace illustrations. No scene
// sequences, changing background, window replacement or returning camera moves.
export const RoundtripFilm = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const t = frame / fps;
  const response = reveal(t, "response");
  const verified = reveal(t, "verify", 1.2);
  const extract = reveal(t, "extract", 1.3);
  const drawer = reveal(t, "skill", 1.2, 1.7);
  const captionClosing = reveal(t, "closing");
  const typedAddress = "http://localhost:8080/api/health";
  const addressChars = Math.floor(
    interpolate(
      t,
      [at("address"), at("address") + 2.2],
      [0, typedAddress.length],
      smooth,
    ),
  );
  return (
    <AbsoluteFill
      style={{
        backgroundColor: petrol,
        fontFamily: '"PingFang SC", "Microsoft YaHei", sans-serif',
        color: foreground,
      }}
    >
      <div
        style={{
          position: "absolute",
          left: 86,
          top: 47,
          display: "flex",
          alignItems: "center",
          gap: 14,
        }}
      >
        <Img src={staticFile("icon.png")} style={{ width: 52, height: 52 }} />
        <span style={{ fontSize: 31, fontWeight: 600 }}>CurlGo</span>
      </div>
      <div
        style={{
          position: "absolute",
          left: 520,
          right: 420,
          top: 53,
          height: 48,
          textAlign: "center",
          fontSize: 34,
        }}
      >
        <span style={{ opacity: interpolate(t, [4.5, 7.5], [1, 0], smooth) }}>
          先把这一条请求检查清楚。
        </span>
        <span
          style={{ position: "absolute", inset: 0, opacity: captionClosing }}
        >
          从你手边的一条请求开始。
        </span>
      </div>
      <div
        style={{
          position: "absolute",
          right: 88,
          top: 62,
          color: "#a4bbb9",
          fontSize: 20,
        }}
      >
        v0.1.1 · 开源 MIT
      </div>
      <div
        style={{
          position: "absolute",
          width: 1440,
          height: 792,
          left: 240,
          top: 141,
          scale: interpolate(
            t,
            [0, manifest.durationSeconds],
            [1, 1.018],
            smooth,
          ),
          transformOrigin: "center center",
          borderRadius: 13,
          overflow: "hidden",
          boxShadow: "0 22px 65px #001a2444",
          border: "1px solid #3d5157",
        }}
      >
        <div
          style={{
            width: 1200,
            height: 660,
            scale: 1.2,
            transformOrigin: "top left",
            position: "relative",
            backgroundColor: panel,
          }}
        >
          <Img
            src={staticFile("curlgo-dark.png")}
            style={{ width: 1200, height: 820, display: "block" }}
          />
          {/* Clear only the cropped sidebar footer using its actual native color. */}
          <div
            style={{
              position: "absolute",
              left: 0,
              top: 628,
              width: 227,
              height: 32,
              background: "#181826",
            }}
          />
          {/* Dim the actual native status/theme controls, keeping their geometry. */}
          <div
            style={{
              position: "absolute",
              left: 920,
              top: 1,
              width: 280,
              height: 55,
              background: "#161724bb",
            }}
          />
          <div
            style={{
              position: "absolute",
              left: 411,
              top: 222,
              width: 635,
              height: 29,
              background: panel,
              display: "flex",
              alignItems: "center",
              fontFamily: mono,
              fontSize: 15,
              color: foreground,
            }}
          >
            {typedAddress.slice(0, addressChars)}
            <span
              style={{
                width: 1,
                height: 18,
                background: mint,
                marginLeft: 2,
                opacity: t < at("send") ? 0.65 : 0,
              }}
            />
          </div>
          <div
            style={{
              position: "absolute",
              left: 1058,
              top: 220,
              width: 113,
              height: 35,
              borderRadius: 7,
              background: "#625df3",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontSize: 14,
            }}
          >
            {t > at("send") + 0.3 && t < at("response")
              ? "发送中 ···"
              : "发送 ⌘ ↵"}
          </div>
          <div
            style={{
              position: "absolute",
              left: 252,
              top: 332,
              right: 22,
              height: 328,
              background: "#11121e",
              borderTop: `1px solid ${border}`,
              padding: "18px 22px",
              boxSizing: "border-box",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 22,
                fontSize: 20,
                marginBottom: 12,
              }}
            >
              <span style={{ fontWeight: 600 }}>响应</span>
              <span
                style={{ fontFamily: mono, color: mint, opacity: response }}
              >
                200 OK
              </span>
              <span style={{ color: muted, fontSize: 15, opacity: response }}>
                42 ms · application/json
              </span>
            </div>
            <div
              style={{
                position: "absolute",
                left: 22,
                top: 84,
                opacity: 1 - response,
                color: muted,
                fontSize: 22,
              }}
            >
              准备就绪，等待这条请求的响应。
            </div>
            <div style={{ opacity: response }}>
              <pre
                style={{
                  fontFamily: mono,
                  fontSize: 22,
                  lineHeight: 1.36,
                  margin: "0 0 12px",
                }}
              >{`{\n  "ok": true,\n  "data": { "id": 42 }\n}`}</pre>
              <div
                style={{
                  opacity: verified,
                  width: 500,
                  borderTop: `1px solid ${border}`,
                  paddingTop: 13,
                  fontSize: 18,
                  display: "flex",
                  justifyContent: "space-between",
                }}
              >
                <span>状态码 = 200</span>
                <span style={{ color: mint }}>✓ 通过</span>
              </div>
              <div
                style={{
                  opacity: verified,
                  width: 500,
                  paddingTop: 9,
                  fontSize: 18,
                  display: "flex",
                  justifyContent: "space-between",
                }}
              >
                <span>业务字段 /ok = true</span>
                <span style={{ color: mint }}>✓ 通过</span>
              </div>
              <div
                style={{
                  opacity: extract,
                  fontSize: 17,
                  color: mint,
                  marginTop: 10,
                  fontFamily: mono,
                }}
              >
                /data/id → id = 42{" "}
                <span style={{ opacity: reveal(t, "next") }}>
                  → 下一条请求可引用
                </span>
              </div>
            </div>
            {/* Ancillary CLI drawer reveals inside this same workbench. */}
            <div
              style={{
                position: "absolute",
                right: 0,
                top: 0,
                width: 340,
                height: 328,
                boxSizing: "border-box",
                padding: "22px 24px",
                opacity: drawer,
                translate: `${interpolate(t, [at("skill") + 1.2, at("skill") + 2.9], [18, 0], smooth)}px 0`,
                borderLeft: `1px solid ${border}`,
                background: "#191c27",
              }}
            >
              <div style={{ fontSize: 17, color: mint, marginBottom: 12 }}>
                可选 API Test Skill
              </div>
              <pre
                style={{
                  fontFamily: mono,
                  fontSize: 17,
                  lineHeight: 1.9,
                  margin: "0 0 19px",
                }}
              >
                {`$ curlgo run\n  --request request.json\n  --dry-run`}
              </pre>
              <div
                style={{
                  opacity: reveal(t, "preflight", 1),
                  color: mint,
                  fontSize: 15,
                  lineHeight: 1.7,
                }}
              >
                ✓ 本地预检通过
                <br />
                此命令不会发送请求
              </div>
              <div
                style={{
                  color: muted,
                  fontSize: 13,
                  lineHeight: 1.6,
                  marginTop: 18,
                }}
              >
                CLI 示意叠层 · 需另行安装 Skill
                <br />
                并提供 CurlGo 可执行文件。
              </div>
            </div>
          </div>
        </div>
      </div>
      <div
        style={{
          position: "absolute",
          left: 94,
          bottom: 113,
          fontSize: 17,
          color: "#8ca7a9",
        }}
      >
        原生深色界面截图 · 地址、响应与交互为示意，非真实录屏
      </div>
      <div
        style={{
          position: "absolute",
          right: 94,
          bottom: 113,
          fontFamily: mono,
          fontSize: 18,
          color: "#a1bbb9",
          opacity: captionClosing,
        }}
      >
        github.com/echoVic/curlgo
      </div>
      <FilmCaptions />
      <Audio src={staticFile("roundtrip.m4a")} />
      <Sequence from={Math.round(manifest.from * fps)} layout="none">
        <Audio src={staticFile(manifest.audio)} />
      </Sequence>
    </AbsoluteFill>
  );
};
