import { interpolate, useCurrentFrame } from "remotion";
import {
  Background,
  Gradient,
  Header,
  Pill,
  Reveal,
  Window,
  colors,
} from "../shared";

export const Native = () => {
  const frame = useCurrentFrame();
  return (
    <Background>
      <Header
        number="01"
        label="原生客户端"
        title={
          <>
            打开，<Gradient>就开始调试。</Gradient>
          </>
        }
        subtitle="HTTP / HTTPS · SSE · WebSocket"
      />
      <Reveal
        delay={18}
        style={{
          position: "absolute",
          left: 105,
          top: 324,
          width: 1155,
          scale: interpolate(frame, [20, 360], [0.97, 1.01], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          }),
        }}
      >
        <Window
          src="curlgo-light.png"
          imageStyle={{
            height: 610,
            objectFit: "cover",
            objectPosition: "top",
          }}
        />
      </Reveal>
      <div style={{ position: "absolute", top: 368, left: 1340, width: 465 }}>
        <Reveal delay={45}>
          <div style={{ fontSize: 51, fontWeight: 760, lineHeight: 1.4 }}>
            粘贴请求
            <br />
            <Gradient>马上开始</Gradient>
          </div>
        </Reveal>
        <Reveal delay={75}>
          <div
            style={{
              fontSize: 28,
              color: colors.muted,
              lineHeight: 1.8,
              marginTop: 24,
            }}
          >
            无需账户或先建项目。
            <br />
            草稿、收藏、历史与 Cookie
            <br />
            保存在本机工作区。
          </div>
        </Reveal>
        <Reveal
          delay={105}
          style={{ display: "flex", flexWrap: "wrap", gap: 16, marginTop: 36 }}
        >
          <Pill>浅色 / 深色</Pill>
          <Pill>环境变量</Pill>
          <Pill>收藏分组</Pill>
        </Reveal>
      </div>
    </Background>
  );
};
