# ADR-0003：前端技术栈 Vue 3 + Vite + TypeScript + Tailwind CSS v4 + shadcn-vue

- 状态：**Accepted**
- 日期：2026-08-01

## 背景（Context）

需要为平台业务提供 Web 控制台（前端）。约束与事实：

- 前端由 Go 后端团队兼职维护，学习曲线要低。
- 页面以管理后台为主：实例/节点/游戏/订单/用户的 CRUD 表格、表单、状态徽章、仪表盘。
- 明确要求使用 Tailwind CSS 做样式。

## 决策（Decision）

前端采用（目录 `platform-web/`，SPA）：

```text
Vue 3（Composition API + <script setup>）
TypeScript（strict）
Vite + @tailwindcss/vite（Tailwind CSS v4）
shadcn-vue（Tailwind 原生组件，复制进代码库）
TanStack Table（服务端分页/筛选/排序表格）
Pinia（状态管理）
Vue Router（路由 + 角色守卫）
vee-validate + zod（表单校验）
ECharts（仪表盘图表）
Vitest + Playwright（测试）
```

**不使用** Element Plus / Ant Design 等传统组件库，因为其自带样式体系与 Tailwind 冲突（preflight 重置、双主题系统、样式覆盖困难）。

## 理由（Why）

- **shadcn 模式**：组件源码直接复制进项目、纯 Tailwind class 编写，无样式冲突、无组件库运行时依赖、完全可定制。
- **Vue 3 选型**：对 Go 团队学习曲线最低，中文生态好；shadcn-vue 已支持 Tailwind v4。
- **Tailwind v4**：CSS-first 配置（`@theme`），`@tailwindcss/vite` 一行接入，不再需要 `tailwind.config.js` + PostCSS。
- **TanStack Table**：无头表格，服务端分页/排序/筛选能力是实例、订单等重表格场景的最优搭配。

## 后果（Consequences）

### Positive

- 样式完全可控、UI 干净现代、暗色模式由 CSS 变量主题原生支持。
- 无组件库版本绑架，组件代码即项目资产。

### Negative / 代价

- 没有“后台大礼包”式整站模板（如 vue-element-admin），登录页/布局/导航需自行拼装（shadcn 组件齐全，工作量可控）。
- shadcn-vue 生态成熟度略逊于 React 版 shadcn/ui，个别组件需自行适配。
