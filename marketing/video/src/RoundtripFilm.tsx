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
const mint = "#42e3b4";
const paper = "#f0fff9";
const muted = "#a1c1bc";
const font = '"PingFang SC", "Microsoft YaHei", sans-serif';
const mono = '"SFMono-Regular", "Menlo", monospace';
const smooth = {
  extrapolateLeft: "clamp" as const,
  extrapolateRight: "clamp" as const,
  easing: Easing.bezier(0.45, 0, 0.2, 1),
};
const at = (id: string) => {
  const beat = manifest.beats.find((b) => b.id === id);
  if (!beat) throw new Error(`Missing audio beat: ${id}`);
  return beat.seconds;
};
const ramp = (t: number, start: number, length = 1.2) =>
  interpolate(t, [start, start + length], [0, 1], smooth);
const windowOpacity = (t: number, start: number, end: number) =>
  ramp(t, start, 0.8) * (1 - ramp(t, end, 0.8));

// One persistent stage and one mounted native workspace. Audio-derived beats
// move the camera and fade detail layers; no scene replaces the background.
const CameraWorkspace = ({ t }: { t: number }) => {
  const motionTimes = [
    0,
    at("workspace"),
    at("request") + 1.5,
    at("local") + 1,
    at("send"),
    at("response") + 1,
    at("verify") + 1,
    at("extract") + 1,
    at("protocols") + 1,
    at("skill") + 1.5,
    at("closing") + 2,
    manifest.durationSeconds,
  ];
  const verify = ramp(t, at("verify"), 0.8);
  const response = ramp(t, at("response") - 1.2, 0.8);
  const extraction = ramp(t, at("extract"), 0.8);
  const send = windowOpacity(t, at("send") + 1.2, at("response") - 0.6);
  return (
    <div
      style={{
        position: "absolute",
        width: 1200,
        height: 820,
        left: interpolate(
          t,
          motionTimes,
          [680, 575, 435, 590, 520, 490, 490, 500, 570, 750, 1060, 1070],
          smooth,
        ),
        top: interpolate(
          t,
          motionTimes,
          [225, 180, 100, 180, 125, 40, 25, 40, 185, 205, 270, 260],
          smooth,
        ),
        scale: interpolate(
          t,
          motionTimes,
          [
            0.82, 0.88, 1.13, 0.87, 1.05, 1.12, 1.13, 1.1, 0.86, 0.77, 0.62,
            0.64,
          ],
          smooth,
        ),
        transformOrigin: "top left",
        borderRadius: 15,
        boxShadow: "0 44px 110px #001b2480, 0 0 0 1px #86bcb34d",
        overflow: "hidden",
        opacity: interpolate(t, [0, 1.5], [0.45, 1], smooth),
      }}
    >
      <Img
        src={staticFile("curlgo-light.png")}
        style={{ display: "block", width: 1200, height: 820 }}
      />
      <div
        style={{
          position: "absolute",
          left: 250,
          top: 214,
          width: 925,
          height: 49,
          border: `3px solid ${mint}`,
          borderRadius: 13,
          boxSizing: "border-box",
          opacity: windowOpacity(t, at("request"), at("local") - 0.7),
          boxShadow: "0 0 0 6px #42e3b41a",
        }}
      />
      <div
        style={{
          position: "absolute",
          left: 1058,
          top: 218,
          width: 113,
          height: 38,
          borderRadius: 8,
          background: "#625df5",
          color: "white",
          fontSize: 15,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          opacity: send,
        }}
      >
        发送中 ···
      </div>
      <div
        style={{
          position: "absolute",
          left: 250,
          top: 327,
          width: 928,
          height: 456,
          boxSizing: "border-box",
          padding: "23px 30px",
          background: "#f5f5fb",
          border: "1px solid #dcdcf0",
          borderRadius: 10,
          opacity: response,
          color: "#182c31",
          boxShadow: "0 9px 25px #1a294314",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            marginBottom: 17,
          }}
        >
          <span style={{ fontSize: 23, fontWeight: 600 }}>响应</span>
          <span style={{ fontSize: 16, color: "#667873" }}>
            接口数据与交互示意
          </span>
        </div>
        <div
          style={{
            display: "flex",
            gap: 25,
            alignItems: "center",
            borderBottom: "1px solid #d7ddeb",
            paddingBottom: 14,
          }}
        >
          <span
            style={{
              color: "#087f78",
              fontFamily: mono,
              fontSize: 23,
              fontWeight: 600,
            }}
          >
            200 OK
          </span>
          <span style={{ fontSize: 17, color: "#667873" }}>
            42 ms · application/json
          </span>
        </div>
        <pre
          style={{
            fontFamily: mono,
            fontSize: 26,
            lineHeight: 1.55,
            margin: "18px 0 13px",
          }}
        >
          {`{\n  "ok": true,\n  "data": { "id": 42 }\n}`}
        </pre>
        <div
          style={{
            opacity: verify,
            borderTop: "1px solid #d7ddeb",
            paddingTop: 13,
          }}
        >
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              fontSize: 20,
              marginBottom: 12,
            }}
          >
            <span>状态码 = 200</span>
            <span style={{ color: "#087f78" }}>✓ 通过</span>
          </div>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              fontSize: 20,
            }}
          >
            <span>业务字段 /ok = true</span>
            <span style={{ color: "#087f78" }}>✓ 通过</span>
          </div>
        </div>
        <div
          style={{
            position: "absolute",
            left: 30,
            right: 30,
            bottom: 18,
            background: "#dff5ea",
            borderRadius: 6,
            padding: "12px 18px",
            opacity: extraction,
            fontSize: 20,
            display: "flex",
            justifyContent: "space-between",
          }}
        >
          <span>验证通过 → 提取 /data/id</span>
          <span style={{ fontFamily: mono, color: "#087f78" }}>
            id = 42 → 下一条请求
          </span>
        </div>
      </div>
    </div>
  );
};

const MarginNotes = ({ t }: { t: number }) => {
  const notes = [
    {
      from: 0,
      to: at("workspace"),
      eyebrow: "一次完整的调试",
      title: (
        <>
          发出请求。
          <br />
          看懂响应。
        </>
      ),
      detail: "再确认，结果对不对。",
    },
    {
      from: at("workspace"),
      to: at("request"),
      eyebrow: "轻量原生",
      title: (
        <>
          打开，
          <br />
          就能开始。
        </>
      ),
      detail: "工作区保存在本机。",
    },
    {
      from: at("request"),
      to: at("local"),
      eyebrow: "从已有请求开始",
      title: (
        <>
          粘贴地址，
          <br />
          或直接导入。
        </>
      ),
      detail: "curl · Postman · Bruno",
    },
    {
      from: at("local"),
      to: at("send"),
      eyebrow: "本地工作区",
      title: (
        <>
          你的请求，
          <br />
          留在手边。
        </>
      ),
      detail: "无需账号\n草稿 · 收藏 · 历史",
    },
    {
      from: at("send"),
      to: at("response"),
      eyebrow: "服务健康检查",
      title: (
        <>
          发出这条
          <br />
          请求。
        </>
      ),
      detail: "GET /api/health",
    },
    {
      from: at("response"),
      to: at("verify"),
      eyebrow: "读懂响应",
      title: (
        <>
          收到 200，
          <br />
          然后呢？
        </>
      ),
      detail: "状态码之后，还有业务结果。",
    },
    {
      from: at("verify"),
      to: at("extract"),
      eyebrow: "声明式断言",
      title: (
        <>
          关键字段，
          <br />
          再确认一次。
        </>
      ),
      detail: "/ok = true",
    },
    {
      from: at("extract"),
      to: at("protocols"),
      eyebrow: "接着往下调试",
      title: (
        <>
          验证通过，
          <br />
          再用变量。
        </>
      ),
      detail: "提取 id → 下一条请求",
    },
    {
      from: at("protocols"),
      to: at("skill"),
      eyebrow: "同一个工作区",
      title: (
        <>
          也能看见
          <br />
          持续的响应。
        </>
      ),
      detail: "HTTP / HTTPS\nSSE · WebSocket",
    },
  ];
  return (
    <>
      {notes.map((note) => (
        <div
          key={note.from}
          style={{
            position: "absolute",
            left: 92,
            top: 298,
            width: 405,
            opacity:
              ramp(t, note.from, 0.7) * (1 - ramp(t, note.to - 0.8, 0.65)),
            translate: `0 ${interpolate(t, [note.from, note.from + 1.5], [16, 0], smooth)}px`,
          }}
        >
          <div style={{ fontSize: 23, color: mint, marginBottom: 29 }}>
            {note.eyebrow}
          </div>
          <div
            style={{
              fontSize: 58,
              letterSpacing: -1.7,
              lineHeight: 1.36,
              fontWeight: 600,
            }}
          >
            {note.title}
          </div>
          <div
            style={{ width: 38, height: 3, background: mint, margin: "34px 0" }}
          />
          <div
            style={{
              fontSize: 25,
              lineHeight: 1.75,
              color: muted,
              whiteSpace: "pre-line",
            }}
          >
            {note.detail}
          </div>
        </div>
      ))}
    </>
  );
};

const SkillPanel = ({ t }: { t: number }) => {
  const start = at("skill");
  const opacity = windowOpacity(t, start, at("closing") - 0.1);
  const preflight = ramp(t, at("preflight"), 1.2);
  return (
    <div
      style={{
        position: "absolute",
        left: 92,
        top: 260,
        width: 590,
        opacity,
        translate: `0 ${interpolate(t, [start, start + 1.5], [24, 0], smooth)}px`,
      }}
    >
      <div style={{ color: mint, fontSize: 23, marginBottom: 28 }}>
        可选 · API Test Skill
      </div>
      <div style={{ fontSize: 52, fontWeight: 600, lineHeight: 1.4 }}>
        需要 AI 帮忙时，
        <br />
        由你主动接入。
      </div>
      <div
        style={{
          marginTop: 36,
          padding: "23px 25px",
          borderRadius: 12,
          background: "#07262e",
          border: "1px solid #497871",
          boxShadow: "0 20px 60px #001b2440",
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            fontSize: 18,
            color: muted,
            marginBottom: 22,
          }}
        >
          <span>命令行 · 本地预检</span>
          <span>流程示意</span>
        </div>
        <pre
          style={{ fontFamily: mono, fontSize: 24, lineHeight: 1.8, margin: 0 }}
        >
          <span style={{ color: mint }}>$ </span>
          {"curlgo run\n  --request request.json\n  --dry-run"}
        </pre>
        <div
          style={{
            opacity: preflight,
            color: mint,
            fontSize: 21,
            marginTop: 22,
          }}
        >
          ✓ 先检查配置，此命令不会发送请求
        </div>
      </div>
      <div
        style={{ fontSize: 21, color: muted, lineHeight: 1.65, marginTop: 25 }}
      >
        技能需另行安装并提供 CurlGo 可执行文件。
        <br />
        桌面端不自动安装或修改 agent 配置。
      </div>
    </div>
  );
};

const LoopPath = ({ t }: { t: number }) => {
  const travel = (t % 12) / 12;
  return (
    <svg
      width={1920}
      height={1080}
      style={{ position: "absolute", opacity: 0.45 }}
    >
      <path
        id="loop"
        d="M135 835H1740Q1810 835 1810 765V206Q1810 136 1740 136H620"
        fill="none"
        stroke="#477871"
        strokeWidth={1.4}
      />
      <path
        d="M135 835H1740Q1810 835 1810 765V206Q1810 136 1740 136H620"
        fill="none"
        stroke={mint}
        strokeWidth={3}
        pathLength={1000}
        strokeDasharray="24 976"
        strokeDashoffset={-travel * 1000}
        opacity={0.5}
      />
    </svg>
  );
};

export const RoundtripFilm = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const t = frame / fps;
  const close = ramp(t, at("closing"), 1.6);
  return (
    <AbsoluteFill
      style={{
        backgroundColor: petrol,
        color: paper,
        fontFamily: font,
        overflow: "hidden",
      }}
    >
      <div
        style={{
          position: "absolute",
          inset: 0,
          background:
            "radial-gradient(ellipse at 70% 40%, #18464b66 0%, #0c303800 64%)",
        }}
      />
      <LoopPath t={t} />
      <div
        style={{
          position: "absolute",
          left: 86,
          top: 49,
          display: "flex",
          alignItems: "center",
          gap: 17,
        }}
      >
        <Img src={staticFile("icon.png")} style={{ width: 61, height: 61 }} />
        <span style={{ fontSize: 35, fontWeight: 650, letterSpacing: -1 }}>
          CurlGo
        </span>
      </div>
      <div
        style={{
          position: "absolute",
          right: 89,
          top: 70,
          fontSize: 20,
          color: muted,
        }}
      >
        你的 API 工具，由你做主。
      </div>
      <div
        style={{
          position: "absolute",
          left: 0,
          right: 0,
          top: 0,
          bottom: 143,
          overflow: "hidden",
          clipPath: "inset(145px 0 0 0)",
        }}
      >
        <CameraWorkspace t={t} />
        <MarginNotes t={t} />
        <SkillPanel t={t} />
        <div
          style={{ position: "absolute", left: 90, top: 250, opacity: close }}
        >
          <Img
            src={staticFile("icon.png")}
            style={{ width: 126, height: 126, marginBottom: 27 }}
          />
          <div
            style={{
              fontSize: 66,
              fontWeight: 600,
              lineHeight: 1.4,
              letterSpacing: -1.6,
            }}
          >
            从你手边的
            <br />
            一条请求开始。
          </div>
          <div style={{ fontSize: 25, marginTop: 30, color: muted }}>
            v0.1.1 · 轻量原生 · 开源 MIT
          </div>
          <div
            style={{
              fontFamily: mono,
              fontSize: 27,
              marginTop: 28,
              color: mint,
            }}
          >
            github.com/echoVic/curlgo ↗
          </div>
        </div>
      </div>
      <div
        style={{
          position: "absolute",
          left: 91,
          bottom: 166,
          fontSize: 18,
          color: muted,
          opacity: ramp(t, 2, 1) * (1 - close),
        }}
      >
        原生界面 · 演示工作区
        {t >= at("response") - 1.2 && t < at("closing")
          ? " / 响应、数据与交互为示意"
          : ""}
      </div>
      <FilmCaptions />
      <Audio src={staticFile("roundtrip.m4a")} />
      <Sequence from={Math.round(manifest.from * fps)} layout="none">
        <Audio src={staticFile(manifest.audio)} />
      </Sequence>
    </AbsoluteFill>
  );
};
