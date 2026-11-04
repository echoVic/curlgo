import { Background, Card, Gradient, Header, Reveal, colors } from "../shared";

export const Agents = () => (
  <Background>
    <Header
      number="04"
      label="可选 Agent 接入"
      title={
        <>
          AI 可以接入，<Gradient>由你决定。</Gradient>
        </>
      }
      subtitle="桌面端不自动安装 agent 技能、插件或 MCP，不修改 agent 配置。"
    />
    <div
      style={{
        position: "absolute",
        top: 350,
        left: 110,
        right: 110,
        display: "flex",
        gap: 38,
      }}
    >
      <Reveal delay={25} style={{ flex: 1 }}>
        <Card style={{ minHeight: 425 }}>
          <div style={{ fontSize: 34, fontWeight: 760 }}>机器可读的 CLI</div>
          <div style={{ fontSize: 26, color: colors.muted, marginTop: 14 }}>
            显式输入，每次执行一个 HTTP 请求
          </div>
          <div
            style={{
              marginTop: 32,
              background: "#f5f6fc",
              border: `1px solid ${colors.border}`,
              padding: 25,
              borderRadius: 12,
              fontFamily: "monospace",
              fontSize: 25,
              lineHeight: 1.85,
            }}
          >
            <span style={{ color: colors.violet }}>$</span> curlgo run
            <br />
            --request request.json
            <br />
            --dry-run
          </div>
          <div style={{ display: "flex", gap: 12, marginTop: 24 }}>
            <span style={{ color: colors.green, fontSize: 25 }}>
              ✓ 本地预检
            </span>
            <span style={{ color: colors.muted, fontSize: 25 }}>
              · 不发送请求
            </span>
          </div>
        </Card>
      </Reveal>
      <Reveal delay={70} style={{ flex: 1 }}>
        <Card style={{ minHeight: 425 }}>
          <div style={{ fontSize: 34, fontWeight: 760 }}>
            主动安装 API Test Skill
          </div>
          <div style={{ fontSize: 26, color: colors.muted, marginTop: 14 }}>
            Claude Code / Codex
          </div>
          <div
            style={{
              marginTop: 32,
              background: "#f5f6fc",
              border: `1px solid ${colors.border}`,
              padding: 25,
              borderRadius: 12,
              fontFamily: "monospace",
              fontSize: 25,
              lineHeight: 1.85,
            }}
          >
            npx skills add echoVic/curlgo
            <br />
            --skill api-test
            <br />
            -a claude-code -a codex
          </div>
          <div style={{ color: colors.muted, marginTop: 24, fontSize: 24 }}>
            先确认目标 agent、安装范围和路径
          </div>
        </Card>
      </Reveal>
    </div>
    <Reveal
      delay={160}
      style={{
        position: "absolute",
        left: 110,
        right: 110,
        bottom: 112,
        fontSize: 26,
        color: colors.muted,
        lineHeight: 1.7,
      }}
    >
      技能需要 CurlGo；缺少二进制时可从可信源码构建。Agent
      读取的数据遵循其自身的数据处理规则。
    </Reveal>
  </Background>
);
