import {
  Background,
  Card,
  Gradient,
  Header,
  Pill,
  Reveal,
  Window,
  colors,
} from "../shared";

export const Assertions = () => (
  <Background>
    <Header
      number="03"
      label="请求与验证"
      title={
        <>
          发送请求，<Gradient>验证真实结果。</Gradient>
        </>
      }
      subtitle="Bearer / Basic / API Key · 声明式断言 · 响应变量提取"
    />
    <Reveal
      delay={20}
      style={{ position: "absolute", left: 110, top: 325, width: 1110 }}
    >
      <Window
        src="http-assertions.png"
        label="CurlGo · 本地测试接口"
        imageStyle={{
          height: 570,
          objectFit: "cover",
          objectPosition: "bottom",
        }}
      />
    </Reveal>
    <div style={{ position: "absolute", top: 350, left: 1290, right: 110 }}>
      <Reveal delay={65}>
        <Card>
          <div style={{ color: colors.muted, fontSize: 24 }}>响应状态</div>
          <div
            style={{
              marginTop: 18,
              fontSize: 68,
              fontWeight: 800,
              color: colors.green,
            }}
          >
            200 OK
          </div>
          <div style={{ marginTop: 12, fontSize: 29, color: colors.muted }}>
            检查状态与业务字段
          </div>
        </Card>
      </Reveal>
      <Reveal delay={100} style={{ marginTop: 24 }}>
        <Card>
          <div style={{ fontSize: 30, fontWeight: 700 }}>✓ 断言通过 2 / 2</div>
          <div
            style={{
              marginTop: 18,
              color: colors.muted,
              fontSize: 25,
              lineHeight: 1.8,
            }}
          >
            状态码符合预期
            <br />
            业务结果符合契约
          </div>
        </Card>
      </Reveal>
      <Reveal delay={155} style={{ marginTop: 25 }}>
        <Pill green>通过后，提取变量</Pill>
      </Reveal>
    </div>
    <Reveal
      delay={200}
      style={{
        position: "absolute",
        bottom: 93,
        left: 110,
        fontSize: 25,
        color: colors.muted,
      }}
    >
      HTTP 200 之外，也检查业务预期；验证失败时阻止响应变量提取。
    </Reveal>
  </Background>
);
