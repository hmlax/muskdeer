# Go Admin 品牌视觉标识

产品统一命名为 **Go Admin**，代码模块和部署标识使用 `go-admin`。

采用旋涡图形标，主色为 `hsl(212 100% 45%)`（约 `#006BE6`），渐变为 `#70B3FF` → `#0D74F2` → `#005AC2`。

## 当前资产

- `vortex.svg`：旋涡图形标。
- `vortex-tile.svg`：512×512 应用图标，导出为 React 的 `public/logo.png`。
- `vortex-favicon.svg`：favicon 图形，导出为 React 的 `public/favicon.ico`。
- `wordmark.svg`：GO ADMIN 几何路径字标，不依赖字体。
- `vortex-lockup.svg`：图形标与 GO ADMIN 字标的横版组合。
- `vortex-login.svg`：登录页插画，与 React 的 `SloganIcon.tsx` 保持一致。
- `preview.html`：图形方案和当前字标预览。
- `brand-render.html`：资产导出画布。

`gust*`、`wmark*` 文件保留为图形方案参考，横版组合统一使用 GO ADMIN 字标。

## 使用规则

图形标保持原有比例和渐变色，不拉伸。图形标四周至少保留其高度四分之一的净空。登录页动效尊重 `prefers-reduced-motion`。

React 的品牌落点为 `src/core/preferences/config/default.ts`、认证页中英文翻译和 `src/components/bussiness/AuthLayout/icons/SloganIcon.tsx`。缓存的应用名、版权名称和链接通过偏好设置版本迁移更新，主题和布局保持不变。
