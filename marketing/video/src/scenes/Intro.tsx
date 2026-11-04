import { Background, Gradient, Logo, Pill, Reveal, colors } from "../shared";

export const Intro = () => (
  <Background>
    <div
      style={{
        position: "absolute",
        inset: "150px 100px 150px",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <Reveal style={{ display: "flex", gap: 22, alignItems: "center" }}>
        <Logo size={100} />
        <div style={{ fontSize: 90, fontWeight: 800, letterSpacing: -3 }}>
          CurlGo
        </div>
      </Reveal>
      <Reveal delay={20}>
        <div
          style={{
            fontSize: 102,
            lineHeight: 1.2,
            fontWeight: 800,
            letterSpacing: -4,
            marginTop: 52,
          }}
        >
          你的 API 工具，<Gradient>由你做主。</Gradient>
        </div>
      </Reveal>
      <Reveal delay={38}>
        <div style={{ marginTop: 30, color: colors.muted, fontSize: 36 }}>
          轻量原生 API 客户端 · 打开即可开始
        </div>
      </Reveal>
      <Reveal delay={55} style={{ display: "flex", gap: 18, marginTop: 48 }}>
        <Pill>无需账号</Pill>
        <Pill>工作区在本地</Pill>
        <Pill>MIT 开源</Pill>
      </Reveal>
      <Reveal delay={75}>
        <div
          style={{
            marginTop: 55,
            fontSize: 23,
            letterSpacing: 5,
            color: colors.muted,
          }}
        >
          FIRST PUBLIC RELEASE / V0.1.0
        </div>
      </Reveal>
    </div>
  </Background>
);
