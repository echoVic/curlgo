import { Background, Gradient, Logo, Pill, Reveal, colors } from "../shared";

export const Outro = () => (
  <Background>
    <div
      style={{
        position: "absolute",
        inset: "150px 100px 140px",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <Reveal style={{ display: "flex", alignItems: "center", gap: 23 }}>
        <Logo size={90} />
        <span style={{ fontSize: 80, fontWeight: 800 }}>
          CurlGo{" "}
          <span style={{ fontSize: 35, color: colors.muted, fontWeight: 500 }}>
            v0.1.0
          </span>
        </span>
      </Reveal>
      <Reveal delay={15}>
        <div
          style={{
            fontSize: 100,
            fontWeight: 800,
            letterSpacing: -4,
            marginTop: 45,
          }}
        >
          <Gradient>现在，开源。</Gradient>
        </div>
      </Reveal>
      <Reveal delay={35}>
        <div style={{ marginTop: 30, fontSize: 38, color: colors.muted }}>
          你的 API 工具，由你做主。
        </div>
      </Reveal>
      <Reveal delay={50}>
        <div
          style={{
            marginTop: 45,
            padding: "22px 38px",
            borderRadius: 17,
            background: "linear-gradient(105deg, #4677f4, #7955e9)",
            color: "white",
            boxShadow: "0 14px 35px #6858ed25",
            fontSize: 38,
            fontWeight: 650,
          }}
        >
          github.com/echoVic/curlgo
        </div>
      </Reveal>
      <Reveal delay={75} style={{ marginTop: 35, display: "flex", gap: 16 }}>
        <Pill>MIT 开源</Pill>
        <Pill>macOS Apple Silicon</Pill>
      </Reveal>
      <Reveal delay={95}>
        <div style={{ fontSize: 25, marginTop: 28, color: colors.muted }}>
          下载 · 导入集合 · 完成你的第一次请求
        </div>
      </Reveal>
    </div>
  </Background>
);
