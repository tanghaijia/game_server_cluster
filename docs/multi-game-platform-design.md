# 多游戏平台支持 —— 需求设计

> 状态：**Approved**（决策已确认）  日期：2026-08-15
> 关联：ADR-0001（platform BFF）/ file-manager-design.md

## 1. 背景与目标

系统当前以单游戏视角运行：Game 仅是基础设施实体（实例关联），用户下单时手填 game_id，前端单主题。目标：

1. **每种游戏有独立的管理页面**（自己的服务器、订单）；
2. 游戏具备**名称、图标、色调**等展示元数据；
3. 用户**切换游戏**来创建、管理服务器；
4. 页面色调随游戏**动态切换**（后续扩展到全主题）。

## 2. 现状盘点

| 项 | 现状 | 差距 |
| --- | --- | --- |
| 游戏数据 | controller `games` 表：ID/Name/AppId/ContainerConfigID | 无图标/色调/展示名 |
| 用户下单 | MyOrdersView 手填 game_id | 无游戏选择概念 |
| 前端主题 | 单一 `--primary`（shadcn/Tailwind CSS 变量） | 无动态切换 |
| 页面 | 全局「我的服务器/我的订单」 | 无游戏作用域 |

## 3. 总体设计

### 3.1 已确认决策

- ✅ **游戏资料存 platform 新表** `game_profiles`（与 controller 基础设施实体解耦）；
- ✅ **子路由游戏空间** `/games/:gameId/*`（URL 可分享）；
- ✅ **图标 admin 上传**到 platform 静态目录 `/static/games/`；
- ✅ **单主色主题**（先覆盖 `--primary`，后续扩展）。

### 3.2 架构分层

```text
controller  games 表  ← 基础设施实体：实例关联、asset_service 同步（不动）

platform    game_profiles 表 ← 产品/呈现目录：展示名、图标、色调、开关、排序
            ├─ 聚合 GET /api/games（controller games + profiles）
            ├─ admin 管理 profile（名称/图标/色调/开关）
            └─ 静态目录 /static/games/{gameId}.png（图标）
前端        /games/:gameId/* 子路由 + 顶部游戏切换器 + CSS 变量主题切换
```

## 4. 详细设计

### 4.1 数据模型（platform 新表 game_profiles）

```sql
CREATE TABLE game_profiles (
    game_id       TEXT PRIMARY KEY,          -- 关联 controller games.ID
    display_name  TEXT NOT NULL,
    icon_url      TEXT NOT NULL DEFAULT '', -- 相对路径 /static/games/{gameId}.png 或外链
    accent_color  TEXT NOT NULL DEFAULT '#6366f1', -- 主题主色（hex/hsl/oklch）
    description   TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,    -- 是否对用户开放
    sort_order    INT NOT NULL DEFAULT 0,
    update_time   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- `game_id` 关联 controller 的 games（无外键约束，跨库）；
- admin 创建游戏 = controller 建 game + platform 建 profile（一个接口包装两步）；
- 未配置 profile 的游戏在用户侧不可见（默认关闭）。

### 4.2 后端接口增量（platform）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/games` | 聚合列表（controller games + profiles，仅 enabled 对用户可见；admin 见全部） |
| GET | `/api/games/:gameId` | 单个游戏详情（含 profile） |
| POST | `/api/admin/games` | 创建游戏：调 controller 建 game + 落 profile（扩展现有） |
| PUT | `/api/admin/games/:id/profile` | 编辑 profile：display_name/icon/accent_color/description/enabled/sort |
| GET | `/api/me/orders?game_id=` | 订单按游戏过滤（现有接口加参数） |
| GET | `/api/me/instances?game_id=` | 实例按游戏过滤（现有接口加参数） |

### 4.3 前端：路由与游戏切换器

```text
/                          → GameLauncherView（游戏卡片列表：图标 + 名称 + 色调底 + 进入）
/games/:gameId/servers     → 该游戏服务器（改造 MyServersView，按 game 过滤）
/games/:gameId/orders      → 该游戏订单（改造 MyOrdersView，game_id 自动带入）
/games/:gameId/settings    → 游戏设置（admin：图标上传/色调选择/开关/描述）
/admin/games               → 游戏管理（现有，跨游戏）
```

- **顶部游戏切换器**：导航栏下拉（当前游戏图标+名称 → 其他游戏），切换即 `router.push`；
- 未进入具体游戏时（如 `/`、admin 页），主题用默认色；
- 我的服务器/我的订单改为游戏作用域（后端 `?game_id=` 过滤）。

### 4.4 主题切换：CSS 变量运行时覆盖

shadcn/Tailwind v4 主题即 CSS 变量（main.css `:root { --primary: ... }`），切换游戏时：

```ts
document.documentElement.style.setProperty('--primary', game.accentColor)
```

- Tailwind `@theme inline` 变量实时生效 → 所有 `bg-primary`/`text-primary-*` 组件自动变色，**现有组件零改动**；
- 色调存储建议 `oklch`（与现有主题变量一致），前端提供 hex↔oklch 换算；
- **对比度校验**：切换前校验主色与前景色对比度，过浅时提示（防止按钮文字不可读）；
- 深色模式：MVP 只覆盖 `--primary`（light/dark 共用），后续再派生次要色/暗色变体。

### 4.5 图标

- admin 上传（png/jpg/svg，≤1MB）→ platform 静态服务 `GET /static/games/{gameId}.png`；
- gin 挂静态目录；文件写 `static/games/`（工作区内，gitignore）；
- 后续可切对象存储（与 node_agent S3 复用）。

## 5. 用户流程（改造后）

```text
登录 → 游戏列表（卡片，游戏色渲染）
  → 点某游戏 → /games/:id/servers
      →「创建服务器」→ 下单（game_id 自动带）→ 支付/开服
      → 服务器列表（该游戏实例 + 文件管理入口）
      → 顶部切换器 → 另一游戏
```

## 6. 里程碑

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| M1 ✅ | game_profiles 表 + 聚合/管理接口 + 静态图标 | **已完成**：聚合 GET /api/games、admin profile 接口、图标上传/静态目录、订单/实例 ?game_id= 过滤；种子（七日杀/饥荒联机版） |
| M2 ✅ | 前端：游戏列表页 + 子路由 + 顶部切换器 + 订单/实例按游戏过滤 | **已完成**：GameLauncherView 卡片列表、/games/:gameId/servers|orders、菜单动态游戏入口、--primary 主题切换 |
| M3 ✅ | 主题切换（--primary 运行时覆盖 + 对比度校验）+ 游戏设置页（图标上传/色调选择） | **已完成**：GameSettingsView（名称/图标上传/色调色板+对比度提示/描述/开关/排序），admin 入口（卡片+侧边栏） |
| M4 | 深色模式主题派生、次要色、游戏内活动/定价等产品属性（可选） | — |

## 7. 风险与边界

- 游戏资料无 profile 时用户侧不可见：需要 seed 或 admin 配置后才能玩；
- 主题色不可控的输入（对比度）需校验兜底；
- 图标上传无鉴权风险：静态目录需防止路径穿越（game_id 白名单校验）；
- 跨游戏数据隔离：订单/实例过滤必须在后端强制（`game_id` 从路由取，不由前端传随意值）。
