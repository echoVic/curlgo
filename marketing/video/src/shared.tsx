import React from "react";
import {
  AbsoluteFill,
  Easing,
  Img,
  interpolate,
  staticFile,
  useCurrentFrame,
} from "remotion";

export const colors = {
  ink: "#171b2c",
  muted: "#697086",
  violet: "#6858ed",
  blue: "#4677f4",
  pink: "#c04bc4",
  green: "#1b9876",
  border: "#e3e5ef",
};
export const font =
  '"PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", sans-serif';
const ease = Easing.bezier(0.16, 1, 0.3, 1);
const clamp = {
  extrapolateLeft: "clamp" as const,
  extrapolateRight: "clamp" as const,
  easing: ease,
};

export const Background: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => (
  <AbsoluteFill
    style={{
      background:
        "radial-gradient(ellipse at 10% 10%, #e9efff 0%, transparent 45%), radial-gradient(ellipse at 90% 85%, #f0eaff 0%, transparent 45%), #f7f9fc",
      color: colors.ink,
      fontFamily: font,
    }}
  >
    {children}
  </AbsoluteFill>
);

export const Reveal: React.FC<{
  children: React.ReactNode;
  delay?: number;
  style?: React.CSSProperties;
}> = ({ children, delay = 0, style }) => {
  const frame = useCurrentFrame();
  return (
    <div
      style={{
        opacity: interpolate(frame, [delay, delay + 24], [0, 1], clamp),
        translate: `0 ${interpolate(frame, [delay, delay + 30], [32, 0], clamp)}px`,
        ...style,
      }}
    >
      {children}
    </div>
  );
};

export const Gradient: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => (
  <span
    style={{
      background: "linear-gradient(100deg, #4677f4, #7955e9 52%, #c64eba)",
      WebkitBackgroundClip: "text",
      backgroundClip: "text",
      color: "transparent",
    }}
  >
    {children}
  </span>
);

export const Header: React.FC<{
  number: string;
  label: string;
  title: React.ReactNode;
  subtitle?: string;
}> = ({ number, label, title, subtitle }) => (
  <div style={{ position: "absolute", left: 108, right: 108, top: 78 }}>
    <Reveal>
      <div
        style={{
          fontSize: 22,
          fontWeight: 650,
          color: colors.muted,
          letterSpacing: 2,
        }}
      >
        CURLGO / {number} {label}
      </div>
    </Reveal>
    <Reveal delay={5}>
      <h1
        style={{
          fontSize: 76,
          lineHeight: 1.2,
          letterSpacing: -2,
          margin: "24px 0 14px",
          fontWeight: 800,
        }}
      >
        {title}
      </h1>
    </Reveal>
    {subtitle && (
      <Reveal delay={12}>
        <div style={{ fontSize: 29, color: colors.muted, lineHeight: 1.6 }}>
          {subtitle}
        </div>
      </Reveal>
    )}
  </div>
);

export const Pill: React.FC<{ children: React.ReactNode; green?: boolean }> = ({
  children,
  green,
}) => (
  <div
    style={{
      display: "inline-flex",
      alignItems: "center",
      gap: 12,
      padding: "15px 24px",
      borderRadius: 14,
      background: "white",
      border: `1px solid ${colors.border}`,
      boxShadow: "0 5px 18px #27375608",
      fontSize: 28,
      fontWeight: 650,
      color: green ? colors.green : colors.ink,
    }}
  >
    {children}
  </div>
);

export const Window: React.FC<{
  src: string;
  style?: React.CSSProperties;
  imageStyle?: React.CSSProperties;
  label?: string;
}> = ({ src, style, imageStyle, label = "CurlGo · 原生界面" }) => (
  <div
    style={{
      borderRadius: 20,
      overflow: "hidden",
      background: "white",
      border: "1px solid #dfe2ed",
      boxShadow: "0 24px 70px #29346c16, 0 4px 14px #29346c09",
      ...style,
    }}
  >
    <div
      style={{
        height: 44,
        background: "#fcfcfe",
        display: "flex",
        alignItems: "center",
        gap: 9,
        padding: "0 18px",
        borderBottom: `1px solid ${colors.border}`,
      }}
    >
      {["#ff625a", "#ffbd2e", "#28c940"].map((color) => (
        <span
          key={color}
          style={{
            width: 11,
            height: 11,
            borderRadius: "50%",
            background: color,
          }}
        />
      ))}
      <span style={{ marginLeft: 12, color: "#8b90a3", fontSize: 17 }}>
        {label}
      </span>
    </div>
    <Img
      src={staticFile(src)}
      style={{ display: "block", width: "100%", ...imageStyle }}
    />
  </div>
);

export const Card: React.FC<{
  children: React.ReactNode;
  style?: React.CSSProperties;
}> = ({ children, style }) => (
  <div
    style={{
      padding: 34,
      borderRadius: 20,
      background: "#ffffffee",
      border: `1px solid ${colors.border}`,
      boxShadow: "0 16px 40px #29346c0a",
      ...style,
    }}
  >
    {children}
  </div>
);

export const Logo: React.FC<{ size?: number }> = ({ size = 84 }) => (
  <Img
    src={staticFile("icon.png")}
    style={{ width: size, height: size, borderRadius: size * 0.22 }}
  />
);

export const Progress = () => {
  const frame = useCurrentFrame();
  const segments = [
    { label: "序章", from: 0, to: 198 },
    { label: "01 原生客户端", from: 198, to: 636 },
    { label: "02 集合迁移", from: 636, to: 954 },
    { label: "03 请求与验证", from: 954, to: 1362 },
    { label: "04 可选 Agent", from: 1362, to: 1800 },
    { label: "开源发布", from: 1800, to: 2100 },
  ];
  return (
    <div
      style={{
        position: "absolute",
        bottom: 30,
        left: 108,
        right: 108,
        display: "flex",
        alignItems: "center",
        gap: 12,
        fontFamily: font,
      }}
    >
      {segments.map((segment) => (
        <div
          key={segment.label}
          style={{
            position: "relative",
            height: 26,
            flex: segment.to - segment.from,
            borderRadius: 5,
            overflow: "hidden",
            border: "1px solid #dce1ef",
            background: "#ffffff80",
          }}
        >
          <div
            style={{
              position: "absolute",
              inset: 0,
              width: `${interpolate(frame, [segment.from, segment.to], [0, 100], { extrapolateLeft: "clamp", extrapolateRight: "clamp" })}%`,
              background: "linear-gradient(90deg, #4677f4, #9654e7)",
            }}
          />
          <span
            style={{
              position: "relative",
              display: "block",
              fontSize: 13,
              textAlign: "center",
              lineHeight: "24px",
              color: frame > segment.from ? "#fff" : "#888fa3",
              fontWeight: 600,
            }}
          >
            {segment.label}
          </span>
        </div>
      ))}
      <div
        style={{
          marginLeft: 12,
          fontSize: 14,
          color: colors.muted,
          whiteSpace: "nowrap",
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {`${Math.floor(frame / 30 / 60)
          .toString()
          .padStart(2, "0")}:${Math.floor((frame / 30) % 60)
          .toString()
          .padStart(2, "0")}`}{" "}
        / 01:10
      </div>
    </div>
  );
};
