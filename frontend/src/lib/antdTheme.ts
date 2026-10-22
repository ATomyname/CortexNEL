/**
 * Ant Design Vue 全局主题（ConfigProvider theme.token）—— Apple HIG 覆盖。
 * 主色 systemBlue #007AFF，成功 #34C759、警告 #FF9500、危险 #FF3B30；
 * 圆角 10–12px；动效 150–250ms；禁止 antd 默认蓝 #1677FF 直出。
 */
import { theme } from 'ant-design-vue'
import type { ThemeName } from './theme'

const FONT_FAMILY =
  '-apple-system, "SF Pro Text", "PingFang SC", "Helvetica Neue", "Microsoft YaHei UI", sans-serif'

/** 两套主题共享的结构 token。 */
const shared = {
  fontFamily: FONT_FAMILY,
  fontSize: 13.5,
  borderRadius: 10,
  borderRadiusLG: 12,
  borderRadiusSM: 8,
  borderRadiusXS: 6,
  controlHeight: 34,
  controlHeightLG: 40,
  controlHeightSM: 28,
  motionDurationFast: '0.15s',
  motionDurationMid: '0.18s',
  motionDurationSlow: '0.25s',
  wireframe: false,
}

/** 浅色（主）—— Apple 系统色板 + 分组底 #F5F5F7，卡片极轻阴影与 1px 分割线。 */
const light = {
  ...shared,
  colorPrimary: '#007AFF',
  colorInfo: '#007AFF',
  colorSuccess: '#34C759',
  colorWarning: '#FF9500',
  colorError: '#FF3B30',
  colorTextBase: '#1D1D1F',
  colorText: '#1D1D1F',
  colorTextSecondary: 'rgba(60, 60, 67, 0.60)',
  colorTextTertiary: 'rgba(60, 60, 67, 0.33)',
  colorTextQuaternary: 'rgba(60, 60, 67, 0.18)',
  colorBgLayout: '#F5F5F7',
  colorBgContainer: '#FFFFFF',
  colorBgElevated: '#FFFFFF',
  colorBgSpotlight: 'rgba(0, 0, 0, 0.85)',
  colorBorder: 'rgba(60, 60, 67, 0.22)',
  colorBorderSecondary: 'rgba(60, 60, 67, 0.12)',
  colorFillAlter: 'rgba(0, 0, 0, 0.02)',
  colorFillSecondary: 'rgba(0, 0, 0, 0.04)',
  boxShadow: '0 1px 3px rgba(0, 0, 0, 0.05)',
  boxShadowSecondary: '0 8px 24px rgba(0, 0, 0, 0.08)',
}

/** 暗色（加分项）—— iOS/macOS 深色系统色板。 */
const dark = {
  ...shared,
  colorPrimary: '#0A84FF',
  colorInfo: '#0A84FF',
  colorSuccess: '#30D158',
  colorWarning: '#FF9F0A',
  colorError: '#FF453A',
  colorTextBase: '#F5F5F7',
  colorText: '#F5F5F7',
  colorTextSecondary: 'rgba(235, 235, 245, 0.60)',
  colorTextTertiary: 'rgba(235, 235, 245, 0.33)',
  colorTextQuaternary: 'rgba(235, 235, 245, 0.18)',
  colorBgLayout: '#111113',
  colorBgContainer: '#1C1C1E',
  colorBgElevated: '#2C2C2E',
  colorBgSpotlight: '#3A3A3C',
  colorBorder: 'rgba(84, 84, 88, 0.65)',
  colorBorderSecondary: 'rgba(84, 84, 88, 0.40)',
  colorFillAlter: 'rgba(255, 255, 255, 0.04)',
  colorFillSecondary: 'rgba(255, 255, 255, 0.08)',
  boxShadow: '0 1px 3px rgba(0, 0, 0, 0.45)',
  boxShadowSecondary: '0 8px 24px rgba(0, 0, 0, 0.50)',
}

export function antdThemeConfig(name: ThemeName) {
  return {
    algorithm: name === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: name === 'dark' ? dark : light,
    components: {
      Card: {
        borderRadiusLG: 12,
        headerHeight: 48,
        headerFontSize: 15,
        colorBorderSecondary: name === 'dark' ? 'rgba(84, 84, 88, 0.35)' : 'rgba(60, 60, 67, 0.10)',
      },
      Table: {
        borderRadius: 10,
        headerBg: name === 'dark' ? 'rgba(255, 255, 255, 0.03)' : 'rgba(0, 0, 0, 0.02)',
        headerColor: name === 'dark' ? 'rgba(235, 235, 245, 0.60)' : 'rgba(60, 60, 67, 0.60)',
        borderColor: name === 'dark' ? 'rgba(84, 84, 88, 0.35)' : 'rgba(60, 60, 67, 0.10)',
      },
      Button: {
        borderRadius: 8,
        borderRadiusLG: 10,
        borderRadiusSM: 6,
        controlHeight: 34,
        controlHeightLG: 40,
        controlHeightSM: 28,
        fontWeight: 600,
      },
      Menu: {
        itemBorderRadius: 8,
        itemMarginInline: 6,
        itemHeight: 38,
      },
      Tabs: {
        titleFontSize: 14,
        cardGutter: 6,
      },
      Modal: {
        borderRadiusLG: 14,
        headerBg: 'transparent',
      },
    },
  }
}
