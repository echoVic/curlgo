import { useCurrentFrame, interpolate } from "remotion";
import {
  Background,
  Card,
  Gradient,
  Header,
  Pill,
  Reveal,
  colors,
} from "../shared";

export const Migration = () => {
  const frame = useCurrentFrame();
  return (
    <Background>
      <Header
        number="02"
        label="集合迁移"
        title={
          <>
            熟悉的请求，<Gradient>带到这里。</Gradient>
          </>
        }
        subtitle="导入前先预览，再加入收藏"
      />
      <div
        style={{
          position: "absolute",
          left: 110,
          right: 110,
          top: 360,
          display: "flex",
          gap: 54,
          alignItems: "center",
        }}
      >
        <Reveal delay={20} style={{ width: 420 }}>
          <Card>
            <div
              style={{ fontSize: 23, color: colors.muted, marginBottom: 25 }}
            >
              导入来源
            </div>
            {["curl 文本", "Postman v2.1", "Bruno 集合"].map((label, i) => (
              <div
                key={label}
                style={{
                  padding: "23px 0",
                  borderBottom:
                    i < 2 ? `1px solid ${colors.border}` : undefined,
                  display: "flex",
                  alignItems: "center",
                  gap: 17,
                  fontSize: 33,
                  fontWeight: 650,
                }}
              >
                <span
                  style={{
                    width: 12,
                    height: 12,
                    borderRadius: 4,
                    background: [colors.blue, "#f28a50", colors.violet][i],
                  }}
                />
                {label}
              </div>
            ))}
          </Card>
        </Reveal>
        <Reveal delay={50} style={{ fontSize: 65, color: colors.violet }}>
          →
        </Reveal>
        <Reveal delay={60} style={{ flex: 1 }}>
          <Card style={{ padding: 0, overflow: "hidden" }}>
            <div
              style={{
                padding: "23px 30px",
                borderBottom: `1px solid ${colors.border}`,
                fontSize: 27,
                fontWeight: 700,
              }}
            >
              导入预览{" "}
              <span
                style={{
                  float: "right",
                  fontSize: 20,
                  color: colors.muted,
                  fontWeight: 400,
                }}
              >
                流程示意
              </span>
            </div>
            {[
              { method: "GET", name: "健康检查", url: "/api/health" },
              { method: "POST", name: "登录请求", url: "/api/login" },
              { method: "GET", name: "用户信息", url: "/api/me" },
            ].map((request, i) => (
              <div
                key={request.name}
                style={{
                  opacity: interpolate(
                    frame,
                    [70 + i * 15, 95 + i * 15],
                    [0, 1],
                    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
                  ),
                  padding: "22px 30px",
                  display: "flex",
                  gap: 25,
                  alignItems: "center",
                  borderBottom: `1px solid ${colors.border}`,
                }}
              >
                <span
                  style={{
                    padding: "7px 12px",
                    borderRadius: 7,
                    width: 88,
                    textAlign: "center",
                    fontSize: 22,
                    fontWeight: 700,
                    background:
                      request.method === "GET" ? "#e6f6ee" : "#fff1e5",
                    color: request.method === "GET" ? colors.green : "#bf7a31",
                  }}
                >
                  {request.method}
                </span>
                <span style={{ fontSize: 29, width: 200 }}>{request.name}</span>
                <span
                  style={{
                    fontSize: 24,
                    color: colors.muted,
                    fontFamily: "monospace",
                  }}
                >
                  {request.url}
                </span>
              </div>
            ))}
            <div
              style={{
                padding: "26px 30px",
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
              }}
            >
              <span style={{ color: colors.muted, fontSize: 24 }}>
                确认内容后保存
              </span>
              <span
                style={{
                  borderRadius: 10,
                  background: colors.violet,
                  padding: "13px 25px",
                  color: "white",
                  fontSize: 25,
                }}
              >
                加入收藏
              </span>
            </div>
          </Card>
        </Reveal>
      </div>
      <Reveal
        delay={150}
        style={{
          position: "absolute",
          left: 110,
          bottom: 140,
          display: "flex",
          alignItems: "center",
          gap: 25,
        }}
      >
        <Pill>集合脚本不执行</Pill>
        <span style={{ fontSize: 27, color: colors.muted }}>
          依赖脚本的工作流，需要另行迁移。
        </span>
      </Reveal>
    </Background>
  );
};
